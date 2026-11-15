import { EventsOn } from '../../../wailsjs/runtime';
import { ListLocalDesktopJobs } from '../../../wailsjs/go/main/App';
import { startVisibleInterval } from '../../utils/visibleInterval';
import { currentInProgressBotTasks, type InProgressBotTask } from './desktopBots';

export interface LocalDesktopJob {
    id?: string;
    title?: string;
    status?: string;
    started_at?: string;
}

const LOCAL_STATUSES = new Set(['deploying', 'upgrading', 'registering']);

// localManagementTasksFrom turns a running desktop install or update into a
// row for the Bot task monitor. The row is marked local so the panel can
// label it （本地管理） instead of a bot name.
export function localManagementTasksFrom(jobs: LocalDesktopJob[] | null | undefined): InProgressBotTask[] {
    const tasks: InProgressBotTask[] = [];
    for (const job of jobs || []) {
        const id = String(job?.id || '').trim();
        const title = String(job?.title || '').trim();
        const status = String(job?.status || '').trim();
        if (!id || !title || !LOCAL_STATUSES.has(status)) continue;
        const started = Date.parse(String(job.started_at || ''));
        const localKind = status === 'upgrading' || status === 'registering' ? status : 'deploying';
        tasks.push({
            id,
            botId: 'local-management',
            text: title,
            phase: 'execute',
            status: 'running',
            startedAt: Number.isFinite(started) ? started : 0,
            local: true,
            localKind,
        });
    }
    return tasks;
}

let cached: InProgressBotTask[] = [];
let subscribers = 0;
let stopPoll: (() => void) | null = null;
let stopEvent: (() => void) | null = null;
const listeners = new Set<() => void>();

export function currentLocalManagementTasks(): InProgressBotTask[] {
    return cached;
}

function sameLocal(left: InProgressBotTask[], right: InProgressBotTask[]): boolean {
    if (left.length !== right.length) return false;
    for (let index = 0; index < left.length; index++) {
        const a = left[index];
        const b = right[index];
        if (a.id !== b.id || a.text !== b.text || a.startedAt !== b.startedAt || a.localKind !== b.localKind) return false;
    }
    return true;
}

let pullGeneration = 0;

async function pullLocalManagementTasks() {
    const generation = ++pullGeneration;
    try {
        const jobs = await ListLocalDesktopJobs();
        if (generation !== pullGeneration) return;
        const next = localManagementTasksFrom(jobs);
        if (sameLocal(cached, next)) return;
        cached = next;
        listeners.forEach(listener => listener());
    } catch {
        // A failed read keeps the last list. Clearing it would hide a job
        // that is still running.
    }
}

export function subscribeLocalManagementTasks(listener: () => void): () => void {
    listeners.add(listener);
    if (subscribers === 0) {
        void pullLocalManagementTasks();
        stopPoll = startVisibleInterval(() => { void pullLocalManagementTasks(); }, 2000);
        try {
            stopEvent = EventsOn('desktop-nodes-changed', () => { void pullLocalManagementTasks(); });
        } catch {
            stopEvent = null;
        }
    }
    subscribers += 1;
    listener();
    return () => {
        listeners.delete(listener);
        subscribers -= 1;
        if (subscribers > 0) return;
        subscribers = 0;
        stopPoll?.();
        stopPoll = null;
        stopEvent?.();
        stopEvent = null;
    };
}

export function tasksForBotMonitor(userId: string, now = Date.now()): InProgressBotTask[] {
    const bots = currentInProgressBotTasks(userId, now);
    if (cached.length === 0) return bots;
    return [...bots, ...cached].sort((left, right) => left.startedAt - right.startedAt || (left.id < right.id ? -1 : left.id > right.id ? 1 : 0));
}
