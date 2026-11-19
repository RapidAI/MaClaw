// One run Hub still lists for a bot. stale means Hub thinks the process is gone.
export type ActiveRunRef = {
    run_id?: string;
    status?: string;
    stale?: boolean;
    phase?: string;
};

// A run Hub still has open. A stale row does not keep the bot busy.
export function activeRunsOccupy(runs: readonly ActiveRunRef[] | null | undefined): boolean {
    return (runs || []).some(row => {
        if (!row || row.stale) return false;
        const status = String(row.status || '').trim();
        return status === '' || status === 'running' || status === 'queued';
    });
}

export type BusyPending = { pending?: boolean; phase?: string; stale: boolean };

// Hub's active runs are the busy signal. The 31-minute clock is used only
// when that read failed. A turn this page just admitted still counts, because
// Hub may not have listed it yet.
export function botBusyPhase(input: {
    hubReachable: boolean;
    runs: readonly ActiveRunRef[] | null | undefined;
    turnLive: boolean;
    pending: readonly BusyPending[];
}): '' | 'plan' | 'execute' {
    if (input.hubReachable) {
        const runs = input.runs || [];
        if (activeRunsOccupy(runs)) {
            if (runs.some(row => row && !row.stale && (row.phase === 'execute' || row.status === 'running'))) return 'execute';
            return 'plan';
        }
        if (input.turnLive) {
            if (input.pending.some(item => item.pending && item.phase === 'execute')) return 'execute';
            return 'plan';
        }
        return '';
    }
    const fresh = (item: BusyPending) => !!item.pending && (input.turnLive || !item.stale);
    if (input.pending.some(item => fresh(item) && item.phase === 'execute')) return 'execute';
    if (input.pending.some(fresh) || input.turnLive) return 'plan';
    return '';
}

type PendingBubble = {
    role: string;
    pending?: boolean;
    id: string;
    runId?: string;
    phase?: string;
};

// A pending bubble that already carries a run id stays pending while Hub
// still lists that same run. A bubble with no run id is not attached to
// whichever run happens to be open.
export function reattachStalePending<T extends PendingBubble>(messages: T[], runs: readonly ActiveRunRef[] | null | undefined): T[] {
    const open = (runs || []).filter(row => row.run_id && !row.stale && (row.status === 'running' || row.status === 'queued' || !String(row.status || '').trim()));
    if (!open.length) return messages;
    let changed = false;
    const next = messages.map(item => {
        if (item.role !== 'assistant' || !item.pending || !item.runId) return item;
        const run = open.find(row => row.run_id === item.runId);
        if (!run) return item;
        const phase = run.phase === 'execute' || run.phase === 'plan' ? run.phase : item.phase;
        if (phase === item.phase) return item;
        changed = true;
        return { ...item, pending: true, phase };
    });
    return changed ? next : messages;
}
