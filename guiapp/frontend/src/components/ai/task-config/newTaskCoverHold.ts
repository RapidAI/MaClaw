/** Cover flag the pending-tab hook can subscribe to.
 *
 * usePendingAssistantTabOpen runs before useTaskConfigWiring, so it cannot
 * read wizardOverlay in the same render. The wiring effect publishes the
 * overlay here, and a change wakes the parked expert open.
 */
let held = false;
const listeners = new Set<() => void>();

export function setNewTaskCoverHeld(next: boolean): void {
    if (held === next) return;
    held = next;
    for (const listener of listeners) listener();
}

export function getNewTaskCoverHeld(): boolean {
    return held;
}

export function subscribeNewTaskCoverHeld(listener: () => void): () => void {
    listeners.add(listener);
    return () => { listeners.delete(listener); };
}

// --- Cover dismissal for surfaces outside the tab strip ---
//
// Remote-machine and cloud conversations do not activate an assistant tab —
// they publish onto the shared remote surface instead, so a task-tab switch's
// dismiss never runs for them. Task opens on those surfaces retire the
// new-task cover the same way a tab switch does. The wiring binds a gated
// dismiss; openers call dismissNewTaskCover.
let dismissCover: (() => void) | null = null;

export function bindNewTaskCoverDismiss(fn: (() => void) | null): void {
    dismissCover = fn;
}

/** A non-tab conversation surface (remote machine task, cloud instance) is
 * opening. Drops the new-task cover when one is painted; no-op otherwise. */
export function dismissNewTaskCover(): void {
    dismissCover?.();
}
