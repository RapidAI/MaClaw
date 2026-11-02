import { visibleTaskRows } from '../ai/codingTaskMode';
import { listedTaskTitle } from '../ai/describeTaskTitle';
import { WORKFLOW_PHASE_META } from '../ai/workflowPhaseMeta.generated';
import {
    isLiveRunningTaskRow,
    liveRunningRowContext,
    taskStatusBucketFor,
    type LiveRunningRowSignals,
    type TaskManagementItem,
} from './SidebarTaskManagement';

/**
 * Wire shape of the buddy `state` frame. The device renders these numbers
 * verbatim; it never derives status from its own vocabulary.
 */
export type BuddyCounts = {
    total: number;
    in_progress: number;
    pending: number;
    paused: number;
    done: number;
    failed: number;
};

export type BuddyTaskItem = {
    id?: string;
    title: string;
    progress: number;
    /** Chinese phase name from the generated workflow catalog. Omitted when unknown. */
    phase?: string;
    /** Age of last activity, already formatted for the glass. Omitted when unknown. */
    elapsed?: string;
};

export type BuddyAttention = {
    kind: 'pending' | 'failed' | 'paused';
    id?: string;
    title: string;
};

export type BuddySnapshot = {
    counts: BuddyCounts;
    current: BuddyTaskItem[];
    attention?: BuddyAttention;
    updated_at: number;
};

/** Glass label for a phase name. Longer catalog names are cut on the wire. */
export const BUDDY_MAX_PHASE_BYTES = 48;

/**
 * The generated catalog calls coding's last phase `review`. The v2 engine
 * still writes `verification` for that same step. Either id maps to it.
 */
const PHASE_ALIASES: Record<string, string> = {
    verification: 'review',
    review: 'verification',
};

export type BuddyPhaseHit = { progress: number; phase: string };

/** Progress and the catalog's display name. Unknown type or phase stays at -1. */
export function buddyPhaseHit(task: TaskManagementItem): BuddyPhaseHit {
    const type = String(task.active_workflow?.type || task.workflow_type || '');
    const phase = String(task.active_workflow?.phase || '');
    const order = WORKFLOW_PHASE_META[type];
    if (!order || order.length === 0 || !phase) return { progress: -1, phase: '' };
    let index = order.findIndex((item) => item.id === phase);
    if (index < 0) {
        const alias = PHASE_ALIASES[phase];
        if (alias) index = order.findIndex((item) => item.id === alias);
    }
    if (index < 0) return { progress: -1, phase: '' };
    const name = truncateUtf8Bytes(String(order[index].name || ''), BUDDY_MAX_PHASE_BYTES);
    return {
        progress: Math.round(((index + 1) / order.length) * 100) / 100,
        phase: name,
    };
}

/** (phase index + 1) / phase count, or -1 when the workflow order is unknown. */
export function buddyTaskProgress(task: TaskManagementItem): number {
    return buddyPhaseHit(task).progress;
}

/**
 * How long since the task last moved. `last_activity` wins; `created_at` is
 * the fallback. The glass shows one bucket so a minute tick can stay quiet
 * until the label itself changes.
 */
export function buddyElapsedLabel(value: string | undefined, now: number): string {
    const timestamp = Date.parse(value || '');
    if (!Number.isFinite(timestamp)) return '';
    const delta = now - timestamp;
    if (delta < 60_000) return '刚刚';
    const minutes = Math.floor(delta / 60_000);
    if (minutes < 60) return `${minutes} 分钟`;
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return `${hours} 小时`;
    return `${Math.floor(hours / 24)} 天`;
}

export const BUDDY_MAX_TITLE_BYTES = 64;
export const BUDDY_MAX_TASKS = 5;

const encoder = new TextEncoder();
const decoder = new TextDecoder();

/** Caps a string at `maxBytes` without splitting a UTF-8 rune. */
export function truncateUtf8Bytes(value: string, maxBytes: number): string {
    if (maxBytes <= 0) return '';
    const bytes = encoder.encode(value);
    if (bytes.length <= maxBytes) return value;
    let end = maxBytes;
    while (end > 0 && (bytes[end] & 0xc0) === 0x80) end--;
    return decoder.decode(bytes.slice(0, end));
}

function fallbackTitle(task: TaskManagementItem): string {
    const path = String(task.project_path || task.working_dir || '').trim();
    if (!path) return '未命名任务';
    const parts = path.split(/[\\/]+/).filter(Boolean);
    return parts[parts.length - 1] || '未命名任务';
}

/**
 * Builds the census the device shows.
 *
 * The partition is built so that `total` always equals the sum of the buckets:
 * a row is counted as in progress when it is live-running *or* bucketed
 * `running`; every other row is counted exactly once by its sidebar bucket.
 * Rows that are both live-running and bucketed pending therefore move to
 * in-progress instead of being counted twice.
 */
export function buildBuddySnapshot(
    tasks: TaskManagementItem[],
    signals?: LiveRunningRowSignals | null,
    now: number = Date.now(),
): BuddySnapshot {
    const rows = visibleTaskRows(Array.isArray(tasks) ? tasks : []);
    const ctx = liveRunningRowContext(signals);

    const runningIdx = new Set<number>();
    rows.forEach((task, index) => {
        if (isLiveRunningTaskRow(task, ctx) || taskStatusBucketFor(task) === 'running') {
            runningIdx.add(index);
        }
    });

    let pending = 0;
    let paused = 0;
    let done = 0;
    let failed = 0;
    rows.forEach((task, index) => {
        if (runningIdx.has(index)) return;
        switch (taskStatusBucketFor(task)) {
            case 'pending':
                pending += 1;
                break;
            case 'paused':
                paused += 1;
                break;
            case 'completed':
                done += 1;
                break;
            default:
                // `other` holds failed, cancelled and blocked rows. The device
                // labels this bucket "异常" rather than claiming every row failed.
                failed += 1;
                break;
        }
    });

    const current: BuddyTaskItem[] = [];
    for (const index of Array.from(runningIdx).slice(0, BUDDY_MAX_TASKS)) {
        const task = rows[index];
        const title = listedTaskTitle(task) || fallbackTitle(task);
        const hit = buddyPhaseHit(task);
        const elapsed = buddyElapsedLabel(task.last_activity || task.created_at, now);
        current.push({
            id: String(task.id || ''),
            title: truncateUtf8Bytes(title, BUDDY_MAX_TITLE_BYTES),
            progress: hit.progress,
            ...(hit.phase ? { phase: hit.phase } : {}),
            ...(elapsed ? { elapsed } : {}),
        });
    }

    const attention = attentionFor(rows, runningIdx);

    const inProgress = runningIdx.size;
    return {
        counts: {
            total: inProgress + pending + paused + done + failed,
            in_progress: inProgress,
            pending,
            paused,
            done,
            failed,
        },
        current,
        ...(attention ? { attention } : {}),
        updated_at: now,
    };
}

/** The one row the glass should name. Pending wins, then a failure, then a pause. */
function attentionFor(
    rows: TaskManagementItem[],
    runningIdx: Set<number>,
): BuddyAttention | undefined {
    let bestRank = 99;
    let best: TaskManagementItem | undefined;
    let bestKind: BuddyAttention['kind'] = 'pending';
    rows.forEach((task, index) => {
        if (runningIdx.has(index)) return;
        const bucket = taskStatusBucketFor(task);
        let rank = 99;
        let kind: BuddyAttention['kind'] = 'failed';
        if (bucket === 'pending') {
            rank = 0;
            kind = 'pending';
        } else if (bucket === 'paused') {
            rank = 2;
            kind = 'paused';
        } else if (bucket !== 'completed' && bucket !== 'running') {
            rank = 1;
            kind = 'failed';
        }
        if (rank < bestRank) {
            bestRank = rank;
            best = task;
            bestKind = kind;
        }
    });
    if (!best) return undefined;
    const title = truncateUtf8Bytes(listedTaskTitle(best) || fallbackTitle(best), BUDDY_MAX_TITLE_BYTES);
    if (!title) return undefined;
    return { kind: bestKind, id: String(best.id || ''), title };
}

/**
 * Task a glass tap should open. A row waiting for confirmation comes first;
 * otherwise the in-progress row at `index` in the snapshot carousel.
 */
/**
 * Row a double-tap may confirm. Only a row waiting for the person qualifies.
 * A live run is not waiting, even when its snapshot still says pending.
 */
export function buddyConfirmTarget(
    tasks: TaskManagementItem[],
    signals?: LiveRunningRowSignals | null,
): TaskManagementItem | undefined {
    const rows = visibleTaskRows(Array.isArray(tasks) ? tasks : []);
    const ctx = liveRunningRowContext(signals);
    return rows.find(
        (task) => !isLiveRunningTaskRow(task, ctx) && taskStatusBucketFor(task) === 'pending',
    );
}

export function buddyFocusTarget(
    tasks: TaskManagementItem[],
    signals?: LiveRunningRowSignals | null,
    index?: number,
): TaskManagementItem | undefined {
    const rows = visibleTaskRows(Array.isArray(tasks) ? tasks : []);
    const ctx = liveRunningRowContext(signals);
    const pending = rows.find(
        (task) => !isLiveRunningTaskRow(task, ctx) && taskStatusBucketFor(task) === 'pending',
    );
    if (pending) return pending;
    if (typeof index !== 'number' || index < 0) return undefined;
    const shown = buildBuddySnapshot(rows, signals).current[index];
    const id = String(shown?.id || '');
    if (!id) return undefined;
    return rows.find((task) => String(task.id || '') === id);
}
