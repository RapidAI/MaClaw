import { RestoreCloudWorkspaceTasks } from '../../wailsjs/go/main/App';

// Startup fires two cloud-task restore triggers within seconds of each other:
// the SidebarTaskManagement mount effect and App's assistant-ready path. Share
// one backend restore so the Hub is asked once and the sidebar progress hint
// appears for a single load instead of two back-to-back ones.
const RESTORE_RESULT_REUSE_MS = 30_000;

let inFlight: Promise<unknown> | null = null;
let settledResult: { value: unknown; at: number } | null = null;
// Bumped on invalidation. A request that started before a workspace mutation
// must not cache its (pre-mutation) result when it settles.
let generation = 0;

export function restoreCloudWorkspaceTasksShared(): Promise<unknown> {
    if (typeof RestoreCloudWorkspaceTasks !== 'function') return Promise.resolve([]);
    if (inFlight) return inFlight;
    if (settledResult && Date.now() - settledResult.at < RESTORE_RESULT_REUSE_MS) {
        return Promise.resolve(settledResult.value);
    }
    const gen = generation;
    const request = Promise.resolve()
        .then(() => RestoreCloudWorkspaceTasks())
        .then(value => {
            if (gen === generation) settledResult = { value, at: Date.now() };
            return value;
        });
    const clearInFlight = () => {
        if (inFlight === request) inFlight = null;
    };
    inFlight = request;
    // Failures are not cached: the next trigger retries the Hub restore.
    request.then(clearInFlight, clearInFlight);
    return request;
}

export function __resetCloudWorkspaceTaskRestoreForTests(): void {
    inFlight = null;
    settledResult = null;
    generation += 1;
}

// Cloud workspace mutations (create/delete/restore) make both a settled result
// and any in-flight request stale. Drop both and bump the generation so the
// old request cannot cache its result when it settles; holders of the old
// promise still receive a value, but the next call starts a fresh restore.
export function invalidateCloudWorkspaceTaskRestore(): void {
    inFlight = null;
    settledResult = null;
    generation += 1;
}
