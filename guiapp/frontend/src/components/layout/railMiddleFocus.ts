import { useSyncExternalStore } from 'react';

/** Middle pane the assistant rail is asking for: task list, or digital employees. */
export type RailMiddleFocus = 'tasks' | 'employees';

// The assistant middle pane unmounts when the shell leaves that page, so a rail
// click can happen before the pane exists. `pending` is the click not yet
// shown. `shown` is the rail highlight. Employees stay unhighlighted until the
// pane confirms the directory can be drawn, so a closed feature gate never
// flashes that item. Tasks can always be drawn, so a tasks click updates the
// highlight immediately and still queues `pending`: a repeat click must wake
// the pane when it is sitting on history.
let shown: RailMiddleFocus = 'tasks';
let pending: RailMiddleFocus | null = null;
const listeners = new Set<() => void>();

function emit() {
    for (const listener of listeners) listener();
}

function subscribe(listener: () => void): () => void {
    listeners.add(listener);
    return () => { listeners.delete(listener); };
}

export function railMiddleFocus(): RailMiddleFocus {
    return shown;
}

export function peekPendingRailMiddleFocus(): RailMiddleFocus | null {
    return pending;
}

export function requestRailMiddleFocus(focus: RailMiddleFocus) {
    // Already queued, or already showing with nothing else waiting.
    if (focus === 'employees' && pending === 'employees') return;
    if (focus === 'employees' && shown === 'employees' && pending === null) return;
    pending = focus;
    if (focus === 'tasks') shown = 'tasks';
    emit();
}

/** The pane is showing this focus. Clears a pending click that this fulfills. */
export function acknowledgeRailMiddleFocus(focus: RailMiddleFocus) {
    const nextPending = pending === focus ? null : pending;
    if (shown === focus && pending === nextPending) return;
    shown = focus;
    pending = nextPending;
    emit();
}

/** Feature hidden while another middle tab is up. Cancels a queued click. */
export function overrideRailMiddleFocus(focus: RailMiddleFocus) {
    if (shown === focus && pending === null) return;
    shown = focus;
    pending = null;
    emit();
}

export function resetRailMiddleFocusForTests() {
    shown = 'tasks';
    pending = null;
}

export function useRailMiddleFocus(): RailMiddleFocus {
    return useSyncExternalStore(subscribe, railMiddleFocus, railMiddleFocus);
}

/** The click the pane has not applied yet. Null once that click is showing. */
export function usePendingRailMiddleFocus(): RailMiddleFocus | null {
    return useSyncExternalStore(subscribe, peekPendingRailMiddleFocus, peekPendingRailMiddleFocus);
}
