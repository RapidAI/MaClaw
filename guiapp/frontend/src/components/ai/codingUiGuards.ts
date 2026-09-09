/** True when focus is in a field that should own Escape (edit plan / password / conflict draft). */
export function isFormFieldTarget(target: EventTarget | null): boolean {
    if (!(target instanceof HTMLElement)) return false;
    const tag = target.tagName;
    if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return true;
    if (target.isContentEditable) return true;
    return !!target.closest("input, textarea, select, [contenteditable='true']");
}

/** True when node is (or is inside) an aria-hidden subtree — e.g. CF slot while SRC tab is active. */
export function isInsideAriaHidden(node: EventTarget | Node | null): boolean {
    if (!(node instanceof Element)) return false;
    return !!node.closest('[aria-hidden="true"]');
}

/**
 * True when the isolation conflict panel is mounted and currently visible
 * (not under a hidden preview tab). Used so float Esc yields only while CF is the active tab.
 */
export function isVisibleCodingConflictPanelPresent(): boolean {
    const el = document.querySelector('[data-testid="coding-conflict-side-panel"]');
    if (!(el instanceof Element)) return false;
    return !isInsideAriaHidden(el);
}
