import { visibleTaskRows } from '../ai/codingTaskMode';
import { listedTaskTitle } from '../ai/describeTaskTitle';
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
};

export type BuddySnapshot = {
    counts: BuddyCounts;
    current: BuddyTaskItem[];
    updated_at: number;
};

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
        current.push({
            id: String(task.id || ''),
            title: truncateUtf8Bytes(title, BUDDY_MAX_TITLE_BYTES),
            // TaskManagementItem carries no numeric progress; -1 tells the
            // device to draw its indeterminate bar instead of inventing 0%.
            progress: -1,
        });
    }

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
        updated_at: now,
    };
}
