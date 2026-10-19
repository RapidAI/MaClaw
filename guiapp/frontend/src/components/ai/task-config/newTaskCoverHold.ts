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
