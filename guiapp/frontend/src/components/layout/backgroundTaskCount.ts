import { TERMINAL_SESSION_STATUSES } from '../remote/types';

export type WorkbenchTaskCounts = {
    background: number;
    scheduled: number;
    passthrough: number;
    /** In-progress bot tasks. Omit when this client has no Bot grant. */
    bot?: number;
};

export function isActiveManageableBackgroundStatus(status: unknown): boolean {
    const normalized = String(status || '').trim().toLowerCase();
    return normalized === 'running' || normalized === 'paused';
}

export function countActiveBackgroundLoops(loops: unknown): number {
    if (!Array.isArray(loops)) return 0;
    return loops.filter((loop: any) => isActiveManageableBackgroundStatus(loop?.status ?? loop?.Status)).length;
}

function sessionStatus(session: any): string {
    return String(session?.status ?? session?.summary?.status ?? '').trim().toLowerCase();
}

function isLiveSession(session: any): boolean {
    return !TERMINAL_SESSION_STATUSES.has(sessionStatus(session));
}

export function isAILaunchedSession(session: unknown): boolean {
    const record = session as { launch_source?: unknown; launchSource?: unknown } | null;
    return String(record?.launch_source ?? record?.launchSource ?? '').trim().toLowerCase() === 'ai';
}

/** Live AI-launched sessions shown on the background monitor tab. */
export function countLiveAISessions(sessions: unknown): number {
    if (!Array.isArray(sessions)) return 0;
    return sessions.filter((session) => session && isAILaunchedSession(session) && isLiveSession(session)).length;
}

/** Non-expired scheduled tasks, matching the scheduled-task list. */
export function countVisibleScheduledTasks(tasks: unknown): number {
    if (!Array.isArray(tasks)) return 0;
    return tasks.filter((task: any) => String(task?.status ?? '').trim().toLowerCase() !== 'expired').length;
}

export function countPassthroughCommands(commands: unknown): number {
    return Array.isArray(commands) ? commands.length : 0;
}

export function formatWorkbenchTaskCountLine(
    counts: WorkbenchTaskCounts,
    labels: { background: string; scheduled: string; passthrough: string; bot?: string },
    separator = ' · ',
): string {
    const parts = [
        `${labels.background} ${Number(counts.background) || 0}`,
        `${labels.scheduled} ${Number(counts.scheduled) || 0}`,
        `${labels.passthrough} ${Number(counts.passthrough) || 0}`,
    ];
    if (typeof counts.bot === 'number') {
        parts.push(`${labels.bot || 'Bot'} ${Number(counts.bot) || 0}`);
    }
    return parts.join(separator);
}
