export const ASSISTANT_OUTPUT_NEAR_BOTTOM_PX = 80;
export const ASSISTANT_REASONING_NEAR_BOTTOM_PX = 24;

export function pinNestedScrollToBottom(el: HTMLElement | null, pinned: boolean): void {
    if (!el || !pinned) return;
    el.scrollTop = el.scrollHeight;
}

export function distanceFromBottom(container: HTMLElement): number {
    return container.scrollHeight - container.scrollTop - container.clientHeight;
}

export function isAwayFromBottom(container: HTMLElement, nearBottomPx = ASSISTANT_OUTPUT_NEAR_BOTTOM_PX): boolean {
    return distanceFromBottom(container) > nearBottomPx;
}

export function tryPinNestedScroll(
    el: HTMLElement | null,
    pinned: boolean,
    userIntent: boolean,
    nearBottomPx = ASSISTANT_REASONING_NEAR_BOTTOM_PX,
): "abandoned" | "pinned" | "skipped" {
    if (!el || !pinned) return "skipped";
    if (userIntent && isAwayFromBottom(el, nearBottomPx)) return "abandoned";
    pinNestedScrollToBottom(el, true);
    return "pinned";
}

export type AssistantScrollIntentEvent = {
    currentTarget?: EventTarget;
    target?: EventTarget | null;
    type?: string;
    deltaY?: number;
};

/** Nested thinking pane: ignore text clicks and downward wheels, but treat scrollbar drags as leave-intent. */
export function shouldIgnoreNestedScrollIntent(event?: AssistantScrollIntentEvent): boolean {
    // A pointerdown on inner text is not leave-intent; the scroller itself is usually the scrollbar.
    if (event?.type === "pointerdown" && event.currentTarget !== event.target) return true;
    // Only an upward wheel leaves the tail. Downward ticks must keep follow alive.
    if (event?.type === "wheel" && (event.deltaY ?? 0) >= 0) return true;
    return false;
}

export function shouldIgnoreUserScrollIntent(event?: AssistantScrollIntentEvent): boolean {
    const target = event?.target;
    if (target instanceof Element && target.closest("[data-nested-scroll]")) return true;
    return shouldIgnoreNestedScrollIntent(event);
}

export function pinAssistantOutputToBottom(
    container: HTMLElement | null,
    endEl: HTMLElement | null,
    behavior: ScrollBehavior,
): void {
    if (container) {
        // Pin the conversation scroller itself. scrollIntoView can also
        // walk ancestor scrollports and is kept as a fallback for the
        // end sentinel after layout settles.
        if (behavior === "smooth" && typeof container.scrollTo === "function") {
            container.scrollTo({ top: container.scrollHeight, behavior: "smooth" });
        } else {
            container.scrollTop = container.scrollHeight;
        }
    }
    endEl?.scrollIntoView({ behavior, block: "end" });
}

export function pinAssistantOutputToTop(container: HTMLElement): void {
    container.scrollTo({ top: 0, behavior: "smooth" });
}

export function tryPinAssistantOutput(
    container: HTMLElement | null,
    endEl: HTMLElement | null,
    behavior: ScrollBehavior,
    userIntent: boolean,
): "abandoned" | "pinned" {
    if (userIntent && container && isAwayFromBottom(container)) return "abandoned";
    pinAssistantOutputToBottom(container, endEl, behavior);
    return "pinned";
}

export function applyUserScrollFollow(userIntent: boolean, awayFromBottom: boolean, userScrolledUp: boolean): { userScrolledUp: boolean; userIntent: boolean } {
    if (userIntent) return { userScrolledUp: awayFromBottom, userIntent: awayFromBottom };
    return { userScrolledUp: awayFromBottom ? userScrolledUp : false, userIntent };
}

export { planConversationFollow, runPinnedScroll } from "./assistantOutputScrollFollow";
