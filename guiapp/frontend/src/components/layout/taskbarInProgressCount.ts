import { cloudWorkspaceIdFromPath, cloudWorkspaceIdFromTaskFields, isExplicitTaskCopy, visibleTaskRows } from '../ai/codingTaskMode';
import { normalizeProjectSessionPath } from '../ai/aiAssistantPanelSessionUtils';
import { isLiveAISession } from './backgroundTaskCount';
import {
    isLiveRunningTaskRow,
    liveRunningRowContext,
    taskStatusBucketFor,
    type LiveRunningRowSignals,
    type TaskManagementItem,
} from './SidebarTaskManagement';

export type TaskbarInProgressInput = {
    tasks?: readonly TaskManagementItem[] | null;
    signals?: LiveRunningRowSignals | null;
    /** In-progress bot tasks. Omit or pass 0 when this client has no Bot grant. */
    botTasks?: number;
    backgroundLoops?: unknown;
    remoteSessions?: unknown;
};

type RunningCoverage = {
    paths: Set<string>;
    workspaceIds: Set<string>;
};

function nonNegativeCount(value: unknown): number {
    const count = typeof value === 'number' ? value : Number(value);
    if (!Number.isFinite(count) || count <= 0) return 0;
    return Math.trunc(count);
}

function addPath(coverage: RunningCoverage, raw: unknown) {
    if (typeof raw !== 'string' || !raw.trim()) return;
    const norm = normalizeProjectSessionPath(raw);
    if (norm) coverage.paths.add(norm);
    const id = cloudWorkspaceIdFromPath(raw);
    if (id) coverage.workspaceIds.add(id);
}

// A session path is the same job when it is the running row or a file inside
// that row. The other direction is not: a workspace root that merely contains
// the task would hide a different project.
function pathCovered(path: string, coverage: RunningCoverage): boolean {
    if (coverage.paths.has(path)) return true;
    for (const known of coverage.paths) {
        if (path.startsWith(`${known}/`)) return true;
    }
    return false;
}

/** One walk: the 进行中 rows, plus the paths those rows already stand for. */
function collectRunningRows(tasks: readonly TaskManagementItem[], signals?: LiveRunningRowSignals | null): { count: number; coverage: RunningCoverage } {
    const ctx = liveRunningRowContext(signals);
    const coverage: RunningCoverage = { paths: new Set(), workspaceIds: new Set() };
    let count = 0;
    for (const task of visibleTaskRows(tasks as TaskManagementItem[])) {
        if (!isLiveRunningTaskRow(task, ctx) && taskStatusBucketFor(task) !== 'running') continue;
        count += 1;
        addPath(coverage, task.project_path);
        // A copy shares the source cache. Covering that path would hide the
        // source's own live session behind the copy's chat.
        if (!isExplicitTaskCopy(task.tags)) {
            addPath(coverage, task.working_dir);
            const rowId = cloudWorkspaceIdFromTaskFields(task);
            if (rowId) coverage.workspaceIds.add(rowId);
        }
    }
    return { count, coverage };
}

function recordString(record: Record<string, unknown>, keys: string[]): string {
    for (const key of keys) {
        const value = record[key];
        if (typeof value === 'string' && value.trim()) return value.trim();
    }
    return '';
}

function sessionLocationPaths(session: unknown): string[] {
    if (!session || typeof session !== 'object') return [];
    const record = session as Record<string, unknown>;
    return [
        record.project_path,
        record.projectPath,
        record.workspace_path,
        record.workspacePath,
        record.workspace_root,
        record.workspaceRoot,
    ].filter((value): value is string => typeof value === 'string' && value.trim() !== '');
}

function sessionAlreadyCovered(session: unknown, coverage: RunningCoverage): boolean {
    for (const raw of sessionLocationPaths(session)) {
        const norm = normalizeProjectSessionPath(raw);
        if (norm && pathCovered(norm, coverage)) return true;
        const id = cloudWorkspaceIdFromPath(raw);
        if (id && coverage.workspaceIds.has(id)) return true;
    }
    return false;
}

function sessionId(session: unknown): string {
    if (!session || typeof session !== 'object') return '';
    return recordString(session as Record<string, unknown>, ['id', 'session_id', 'sessionId']);
}

/** Live AI sessions that are not the same job as a task row already counted. */
function extraLiveAISessions(sessions: unknown, coverage: RunningCoverage): { count: number; accountedIds: Set<string> } {
    const accountedIds = new Set<string>();
    if (!Array.isArray(sessions)) return { count: 0, accountedIds };
    let count = 0;
    for (const session of sessions) {
        const live = isLiveAISession(session);
        const covered = sessionAlreadyCovered(session, coverage);
        if (!live && !covered) continue;
        const id = sessionId(session);
        if (id) accountedIds.add(id);
        if (live && !covered) count += 1;
    }
    return { count, accountedIds };
}

function loopStatus(loop: unknown): string {
    if (!loop || typeof loop !== 'object') return '';
    return recordString(loop as Record<string, unknown>, ['status', 'Status']).toLowerCase();
}

function loopSessionId(loop: unknown): string {
    if (!loop || typeof loop !== 'object') return '';
    return recordString(loop as Record<string, unknown>, ['session_id', 'sessionId', 'SessionID']);
}

/** Running loops only. A paused loop is not in progress, and a loop whose
 * remote session is already in the count must not be added again. */
function extraRunningLoops(loops: unknown, accountedSessionIds: Set<string>): number {
    if (!Array.isArray(loops)) return 0;
    let count = 0;
    for (const loop of loops) {
        if (loopStatus(loop) !== 'running') continue;
        const session = loopSessionId(loop);
        if (session && accountedSessionIds.has(session)) continue;
        count += 1;
    }
    return count;
}

/**
 * Tasks still in progress for the Windows taskbar badge.
 * Task-list 进行中 rows, in-progress bot tasks, running background loops,
 * and live AI sessions are separate monitors. A session or loop is skipped
 * when a running row or that session already represents the same job.
 */
export function taskbarInProgressCount(input: TaskbarInProgressInput = {}): number {
    const tasks = Array.isArray(input.tasks) ? input.tasks : [];
    const { count: rows, coverage } = collectRunningRows(tasks, input.signals);
    const { count: sessions, accountedIds } = extraLiveAISessions(input.remoteSessions, coverage);
    return rows
        + nonNegativeCount(input.botTasks)
        + extraRunningLoops(input.backgroundLoops, accountedIds)
        + sessions;
}
