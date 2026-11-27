import { cssPerVisualPx } from "./conversationRenderWindow";

/** The transcript is not the visible view. scrollHeight would lay out a tall hidden tree. */
export function outputHostHidden(container: HTMLElement | null): boolean {
    return !!container?.closest("[hidden]");
}

/** The live thought is the last nested pane. Older turns stay unread. */
export function lastNestedScroll(root: HTMLElement): HTMLElement | null {
    let node: Element | null = diveToLastElement(root);
    while (node) {
        if (node instanceof HTMLElement && node.hasAttribute("data-nested-scroll")) return node;
        if (node === root) return null;
        const previous: Element | null = node.previousElementSibling;
        if (previous) {
            node = diveToLastElement(previous);
            continue;
        }
        node = node.parentElement;
    }
    return null;
}

function diveToLastElement(start: Element): Element {
    let node = start;
    let child = node.lastElementChild;
    while (child) {
        node = child;
        child = node.lastElementChild;
    }
    return node;
}

/**
 * The open thought pane is max-height capped. Once it scrolls internally,
 * more reasoning does not change the outer transcript height. Reading the
 * outer scrollHeight would lay out every bubble above it.
 */
export function cappedThoughtAbsorbsGrowth(container: HTMLElement | null): boolean {
    const last = container ? lastNestedScroll(container) : null;
    if (!last) return false;
    return last.scrollHeight > last.clientHeight + 1;
}

/** The direct child of the transcript host that contains the user row. */
export function transcriptAnchorElement(host: Element, node: HTMLElement): HTMLElement {
    let current: HTMLElement = node;
    while (current.parentElement && current.parentElement !== host && host.contains(current.parentElement)) {
        const parent = current.parentElement;
        if (!(parent instanceof HTMLElement)) break;
        current = parent;
    }
    return current;
}

/** CSS-pixel offset of a row from the scrollport top. Visual rects scale with `--ui-scale`; this does not. */
export function anchorCssRelTop(el: HTMLElement, container: HTMLElement): number {
    return (el.getBoundingClientRect().top - container.getBoundingClientRect().top) * cssPerVisualPx(container);
}

export function childIndexOf(host: Element, el: Element): number {
    const kids = host.children;
    for (let index = 0; index < kids.length; index += 1) {
        if (kids[index] === el) return index;
    }
    return -1;
}
