export const MENU_MIN_WIDTH = 176;
export const MENU_MAX_HEIGHT = 360;
export const MENU_MIN_INTERACTIVE_HEIGHT = 44;

/** Place the menu on the roomier side and constrain it to the viewport. */
export function clampMenuPosition(
    triggerRect: { left: number; top: number; bottom: number; width: number },
    viewport: { width: number; height: number } = typeof window !== "undefined"
        ? { width: window.innerWidth, height: window.innerHeight }
        : { width: 1280, height: 800 },
): { left: number; top: number; openUp: boolean; maxHeight: number } {
    const pad = 8;
    const gap = 6;
    const spaceAbove = Math.max(0, triggerRect.top - gap - pad);
    const spaceBelow = Math.max(0, viewport.height - triggerRect.bottom - gap - pad);
    const openUp = spaceAbove >= MENU_MAX_HEIGHT || spaceAbove >= spaceBelow;
    const maxHeight = Math.min(MENU_MAX_HEIGHT, openUp ? spaceAbove : spaceBelow);
    const maxLeft = Math.max(pad, viewport.width - MENU_MIN_WIDTH - pad);
    const clampedLeft = Math.min(Math.max(pad, triggerRect.left), maxLeft);
    if (openUp) {
        return { left: clampedLeft, top: triggerRect.top - gap, openUp: true, maxHeight };
    }
    return {
        left: clampedLeft,
        top: Math.min(viewport.height - pad, triggerRect.bottom + gap),
        openUp: false,
        maxHeight,
    };
}

export function menuItems(menu: HTMLElement): HTMLButtonElement[] {
    return Array.from(menu.querySelectorAll<HTMLButtonElement>('[role="menuitem"], [role="menuitemradio"]')).filter(
        (item) => !item.disabled,
    );
}

export function focusMenuItem(menu: HTMLElement, index: number) {
    const items = menuItems(menu);
    if (items.length === 0) return;
    const next = ((index % items.length) + items.length) % items.length;
    items[next]?.focus();
}
