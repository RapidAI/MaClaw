import { desktopBotLocalControl, desktopBotStopIntent, isDesktopBotClearCommand, pendingBotReplyIsStale, BOT_CONFIRM_TASK } from './desktopBots';

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

// A pending bubble past the clock stays pending when Hub still lists its run.
// The run id is written on the bubble so a later poll keeps the same match.
export function reattachStalePending<T extends PendingBubble>(messages: T[], runs: readonly ActiveRunRef[] | null | undefined, now = Date.now()): T[] {
    const open = (runs || []).filter(row => row.run_id && !row.stale && (row.status === 'running' || row.status === 'queued' || !String(row.status || '').trim()));
    if (!open.length) return messages;
    const used = new Set(messages.map(item => item.runId).filter((id): id is string => !!id));
    let changed = false;
    const next = messages.map(item => {
        if (item.role !== 'assistant' || !item.pending) return item;
        if (item.runId && open.some(row => row.run_id === item.runId)) return item;
        if (!pendingBotReplyIsStale(item, now)) return item;
        const run = open.find(row => row.run_id && !used.has(row.run_id));
        if (!run?.run_id) return item;
        used.add(run.run_id);
        changed = true;
        const phase = run.phase === 'execute' || run.phase === 'plan' ? run.phase : item.phase;
        return { ...item, pending: true, runId: run.run_id, phase };
    });
    return changed ? next : messages;
}

// A new line waits behind a run Hub still lists. Stop, status, and clear
// still go straight through.
export function messageQueuesBehindRun(content: string, occupy: boolean): boolean {
    const text = content.trim();
    if (!occupy || !text) return false;
    if (text === BOT_CONFIRM_TASK) return false;
    if (isDesktopBotClearCommand(text)) return false;
    if (desktopBotStopIntent(text)) return false;
    if (desktopBotLocalControl(text)) return false;
    return true;
}
