import { useEffect, useId, useRef, useState, type ReactNode } from 'react';
import { useSafeBackdropDismiss } from '../../hooks/useSafeBackdropDismiss';
import { useModalFocusTrap } from '../../hooks/useModalFocusTrap';

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
        /** Full wording, used on the confirm step's action button. */
        button: string;
        /** Short verb for the card's own button, where the title already says the action. */
        short?: string;
        /** Plain-language explanation, shown under the button and again in the confirm step. */
        description: string[];
        confirmTitle: string;
        confirmButton: string;
        forceButton: string;
        working: string;
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
    /** Whether `notice` reports a finished action or a failure. */
    noticeTone?: 'success' | 'error';
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
export function BotComputerSettingsDialog({ labels, state, working, busy, notice, noticeTone = 'error', tabs, onClose, onRun, onCancelConfirm }: Props) {
    const titleId = useId();
    const [confirm, setConfirm] = useState<DesktopComputerAction | null>(null);
    const [ack, setAck] = useState(false);
    const running = working !== null;
    const dialogRef = useRef<HTMLDivElement | null>(null);
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose, { enabled: !running });
    // Escape backs out one step at a time: first the confirm step, then the
    // dialog. Tab is wrapped by the trap, as on the other two tabs.
    const { onKeyDown } = useModalFocusTrap(dialogRef, {
        onEscape: () => {
            if (running) return;
            if (confirm || busy) { setConfirm(null); setAck(false); onCancelConfirm(); }
            else onClose();
        },
    });

    // Swapping the body unmounts whatever held focus, and focus falls to <body>
    // without any event. Move it deliberately instead.
    //
    // The view is identified by *what is showing*, not by "is this the confirm
    // step". Starting an action runs setConfirm(null) and setWorking(action) as
    // two commits: the first paints the overview (focus goes back to the
    // opener), the second paints the confirm step again — and that opener is
    // gone, so focus would land on <body>. Comparing the actual view catches
    // both commits; a boolean would only see the first.
    const askedFromRef = useRef<DesktopComputerAction | null>(null);
    const viewRef = useRef<string>('');

    const ask = (action: DesktopComputerAction) => {
        askedFromRef.current = action;
        setAck(false);
        setConfirm(action);
    };

    const pending = confirm ?? working ?? busy;
    const showConfirm = pending !== null;
    // The view key must tell "waiting for an answer" from "running": the action
    // button that held focus is disabled and then unmounted when the action
    // starts, so that transition needs its own focus move even though both
    // states render the confirm step.
    const view = showConfirm ? `confirm:${pending}:${running ? 'running' : 'idle'}` : 'overview';

    useEffect(() => {
        const dialog = dialogRef.current;
        if (!dialog || view === viewRef.current) return;
        const first = viewRef.current === '';
        viewRef.current = view;
        if (first) return; // The trap already focused the dialog on mount.
        if (showConfirm) {
            // Running disables every control, so the container is the only
            // thing left to hold focus until the action reports back.
            const target = dialog.querySelector<HTMLElement>(
                '[data-testid="bot-computer-reset-ack"], [data-testid^="bot-computer-confirm-"][data-testid$="-go"]',
            );
            (running || !target ? dialog : target).focus({ preventScroll: true });
            return;
        }
        const back = askedFromRef.current;
        askedFromRef.current = null;
        // After the action reports back, the next thing to do is close the
        // dialog, not act again — landing on the action button would invite a
        // second restart. A plain cancel goes back to the button that asked.
        const next = notice && noticeTone === 'success'
            ? dialog.querySelector<HTMLElement>('.custom-dialog__footer .btn-secondary:not(.btn-danger)')
            : (back ? dialog.querySelector<HTMLElement>(`[data-testid="bot-computer-${back}"]`) : null);
        (next ?? dialog).focus({ preventScroll: true });
    }, [view, showConfirm, running, notice, noticeTone, dialogRef]);

    const action = pending ? labels.actions[pending] : null;
    const canConfirm = !running && (pending !== 'reset' || ack);

    return (
        <div className="modal-backdrop custom-dialog-backdrop bot-llm-dialog-backdrop" {...backdropProps} onKeyDown={onKeyDown}>
            <div
                className="modal-content custom-dialog bot-llm-dialog"
                ref={dialogRef}
                role="dialog"
                aria-modal="true"
                aria-labelledby={titleId}
                data-testid="bot-computer-dialog"
                tabIndex={-1}
                {...dialogProps}
            >
                <div className="bot-llm-dialog__head">
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
                    {/* The confirm step owns the dialog, so the tab strip would
                        only offer a way out of an unanswered question. */}
                    {showConfirm ? null : tabs}
                </div>
                {showConfirm && action && pending ? (
                    <>
                        <div className="modal-body custom-dialog__body" data-testid={`bot-computer-confirm-${pending}`}>
                            <h4 className={`bot-llm-dialog__section${pending === 'reset' ? ' bot-computer__danger-title' : ''}`}>{action.confirmTitle}</h4>
                            <Description lines={action.description} />
                            <p className="bot-computer__shared">{labels.shared}</p>
                            {state.active_runs > 0 ? <p className="bot-computer__progress" role="status">{labels.activeRuns(state.active_runs)}</p> : null}
                            {busy === pending && !running ? <p className="bot-computer__warn" role="alert" data-testid="bot-computer-busy">{labels.busy}</p> : null}
                            {pending === 'reset' && !running ? (
                                <label className="bot-llm-dialog__radio bot-computer__ack">
                                    <input type="checkbox" checked={ack} onChange={e => setAck(e.target.checked)} data-testid="bot-computer-reset-ack" />
                                    <span>{action.confirmButton}</span>
                                </label>
                            ) : null}
                            {running ? <p className="bot-computer__progress" role="status" data-testid="bot-computer-working">{action.working}</p> : null}
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
                            <p className="bot-computer__shared">{labels.shared}</p>
                            {(['restart', 'reset'] as DesktopComputerAction[]).map(item => {
                                const copy = labels.actions[item];
                                const disabled = running || (item === 'reset' ? !state.reset_allowed : !state.restart_allowed);
                                return (
                                    <section
                                        className={`bot-computer__action${item === 'reset' ? ' is-danger' : ''}`}
                                        key={item}
                                        aria-label={copy.name}
                                    >
                                        <div className="bot-computer__action-head">
                                            <h5 className="bot-computer__action-title">{copy.name}</h5>
                                            <button
                                                type="button"
                                                className={item === 'reset' ? 'btn-secondary btn-danger' : 'btn-secondary'}
                                                data-testid={`bot-computer-${item}`}
                                                disabled={disabled}
                                                onClick={() => ask(item)}
                                            >
                                                {copy.short ?? copy.button}
                                            </button>
                                        </div>
                                        <Description lines={copy.description} />
                                        {item === 'reset' && !state.reset_allowed ? <p className="bot-llm-dialog__subhint">{labels.resetDisabled}</p> : null}
                                    </section>
                                );
                            })}
                            {notice ? <p className={noticeTone === 'success' ? 'bot-computer__done' : 'bot-llm-dialog__notice'} role="status" data-testid="bot-computer-notice">{notice}</p> : null}
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
