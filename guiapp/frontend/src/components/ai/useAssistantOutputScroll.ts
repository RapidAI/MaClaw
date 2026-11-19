import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import type { ChatMessage } from "./useAIAssistant";
import { logAIScrollEvent, logAIScrollSnapshot, scrollSnapshotDue } from "./assistantScrollDiag";
import {
    ASSISTANT_OUTPUT_NEAR_BOTTOM_PX,
    applyUserScrollFollow,
    isAwayFromBottom,
    pinAssistantOutputToTop,
    planConversationFollow,
    runPinnedScroll,
    shouldIgnoreUserScrollIntent,
    tryPinAssistantOutput,
    type AssistantScrollIntentEvent,
} from "./assistantOutputScrollLogic";
import { locatorCssEscape, userTurnScrollTop } from "./chatSessionLocatorModel";

export { ASSISTANT_OUTPUT_NEAR_BOTTOM_PX };

/** The transcript is not the visible view. scrollHeight would lay out a tall hidden tree. */
function outputHostHidden(container: HTMLElement | null): boolean {
    return !!container?.closest("[hidden]");
}

/**
 * The open thought pane is max-height capped. Once it scrolls internally,
 * more reasoning does not change the outer transcript height. Reading the
 * outer scrollHeight would lay out every bubble above it.
 */
function cappedThoughtAbsorbsGrowth(container: HTMLElement | null): boolean {
    const last = container ? lastNestedScroll(container) : null;
    if (!last) return false;
    return last.scrollHeight > last.clientHeight + 1;
}

/** The direct child of the transcript host that contains the user row. */
function transcriptAnchorElement(host: Element, node: HTMLElement): HTMLElement {
    let current: HTMLElement = node;
    while (current.parentElement && current.parentElement !== host && host.contains(current.parentElement)) {
        const parent = current.parentElement;
        if (!(parent instanceof HTMLElement)) break;
        current = parent;
    }
    return current;
}

function childIndexOf(host: Element, el: Element): number {
    const kids = host.children;
    for (let index = 0; index < kids.length; index += 1) {
        if (kids[index] === el) return index;
    }
    return -1;
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

/** The live thought is the last nested pane. Older turns stay unread. */
function lastNestedScroll(root: HTMLElement): HTMLElement | null {
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

/**
 * The follow logic only reads identity and a length hint, so any transcript
 * matching this shape can use it — including surfaces that keep their own
 * message type (the file companion stores the body in `text`, not `content`).
 */
export type AssistantOutputScrollMessage = Pick<ChatMessage, "id" | "role"> & { content?: string; hostInitiated?: boolean };

interface AssistantOutputScrollOptions {
    activityKey?: string;
    conversationKey?: string;
    hasConversation: boolean;
    messages: AssistantOutputScrollMessage[];
    ready: boolean;
    scrollToTopSeq?: number;
    /** While set, skip scrollHeight reads. The new-task cover stays up over a tall transcript. */
    suspendedRef?: { readonly current: boolean };
}

export function useAssistantOutputScroll({ activityKey, conversationKey = "", hasConversation, messages, ready, scrollToTopSeq, suspendedRef }: AssistantOutputScrollOptions) {
    const outputEndRef = useRef<HTMLDivElement | null>(null);
    const outputContainerRef = useRef<HTMLDivElement | null>(null);
    const userScrolledUpRef = useRef(false);
    const userIntentRef = useRef(false);
    const prevMsgCountRef = useRef(0);
    const prevLastUserMsgIdRef = useRef<string | undefined>(undefined);
    const prevConversationShapeRef = useRef<{ key: string; firstId?: string; tailId?: string }>({ key: conversationKey });
    const prevActivityKeyRef = useRef(activityKey);
    const scrollRafRef = useRef<number | null>(null);
    const prevReadyRef = useRef(ready);
    const programmaticScrollRef = useRef(false);
    const wasCoveredRef = useRef(false);
    const prevTailStampRef = useRef("");
    const thoughtPaneCappedRef = useRef(false);
    const resizeObserverRef = useRef<ResizeObserver | null>(null);
    const resizeRafRef = useRef<number | null>(null);
    // Rendered mirror of the scrolled-up latch plus a "new tail while latched"
    // flag, so the panel can offer a jump-to-latest pill while auto-follow is
    // off. Without it a reader parked mid-transcript has no hint that the
    // turn (or a goal's completion report) kept growing below the fold.
    const [scrolledUp, setScrolledUp] = useState(false);
    const [unreadTail, setUnreadTail] = useState(false);
    const latchedTailStampRef = useRef("");
    // Viewport anchor for a scrolled-up reader: the first transcript row
    // whose bottom edge is inside the viewport, plus its distance to the
    // scrollport top, its position among the host's children, and its
    // data-testid (which carries the message id) for an exact remount match.
    // Chromium's own overflow-anchor loses the anchor whenever React
    // re-projects the transcript (history merge, archive-window shift, a
    // transient collapsed commit): the anchor node is detached and the
    // scroller drops the reader at scrollTop ~0 — that is how a latched
    // view ended up 15,000px above the goal closeout that kept streaming
    // below the fold.
    const anchorRef = useRef<{ el: Element | null; relTop: number; index: number; testId: string | null } | null>(null);

    const commitScrolledUp = useCallback((up: boolean) => {
        const wasLatched = userScrolledUpRef.current;
        userScrolledUpRef.current = up;
        setScrolledUp(prev => (prev === up ? prev : up));
        if (up) {
            // The previous effect run's tail stamp is the latch-time baseline:
            // anything that changes the tail afterwards counts as unread.
            // Repeated commits (every abandoned pin re-asserts the latch)
            // must not move the baseline forward and swallow arrivals the
            // reader has not seen.
            if (!wasLatched) latchedTailStampRef.current = prevTailStampRef.current;
        } else {
            setUnreadTail(false);
            latchedTailStampRef.current = "";
        }
    }, []);

    const clearScrolledUp = useCallback(() => {
        userIntentRef.current = false;
        commitScrolledUp(false);
        anchorRef.current = null;
    }, [commitScrolledUp]);

    const cancelScheduledScroll = useCallback(() => {
        if (scrollRafRef.current === null || typeof cancelAnimationFrame !== "function") return;
        cancelAnimationFrame(scrollRafRef.current);
        scrollRafRef.current = null;
    }, []);

    const containerMetrics = useCallback(() => {
        const container = outputContainerRef.current;
        if (!container) return { sh: -1, ch: -1, st: -1, dist: -1 };
        return {
            sh: container.scrollHeight,
            ch: container.clientHeight,
            st: Math.round(container.scrollTop),
            dist: Math.round(container.scrollHeight - container.scrollTop - container.clientHeight),
        };
    }, []);

    // The transcript host lookup walks the subtree once and is cached —
    // captureAnchor runs per scroll event, so a fresh querySelector each
    // time would re-descend the whole transcript on every tick.
    const anchorHostCacheRef = useRef<{ container: HTMLElement; host: Element } | null>(null);
    const anchorHost = useCallback((container: HTMLElement): Element => {
        const cached = anchorHostCacheRef.current;
        if (cached && cached.container === container && cached.host.isConnected && container.contains(cached.host)) {
            return cached.host;
        }
        const host = container.querySelector(".mc-conversation-body") ?? container;
        anchorHostCacheRef.current = { container, host };
        return host;
    }, []);

    const captureAnchor = useCallback(() => {
        const container = outputContainerRef.current;
        // The anchor only matters while a reader is parked away from the
        // tail; while the view follows, measuring rows is pure cost.
        if (!container || !userScrolledUpRef.current || outputHostHidden(container)) return;
        const top = container.getBoundingClientRect().top;
        const host = anchorHost(container);
        const kids = host.children;
        // The stored anchor is refreshed on every scroll event, so by the
        // next commit it is normally still at the viewport edge — walk from
        // it instead of rescanning the whole transcript per commit.
        const stored = anchorRef.current?.el;
        let el: Element | null = stored && stored.isConnected && stored.parentElement === host ? stored : (kids[0] ?? null);
        while (el && (!(el instanceof HTMLElement) || el.getBoundingClientRect().bottom <= top)) {
            el = el.nextElementSibling;
        }
        while (el) {
            const sib: Element | null = el.previousElementSibling;
            if (!(sib instanceof HTMLElement) || sib.getBoundingClientRect().bottom <= top) break;
            el = sib;
        }
        // An empty commit leaves nothing to anchor on. Keep the previous
        // element/index so the next non-empty commit can still restore —
        // otherwise a transient collapse drops the reader at scrollTop 0.
        if (!(el instanceof HTMLElement)) return;
        anchorRef.current = { el, relTop: el.getBoundingClientRect().top - top, index: childIndexOf(host, el), testId: el.getAttribute("data-testid") };
    }, [anchorHost]);

    const restoreAnchor = useCallback(() => {
        const container = outputContainerRef.current;
        const anchor = anchorRef.current;
        if (!container || !anchor || outputHostHidden(container)) return;
        const host = anchorHost(container);
        const kids = host.children;
        let el: Element | null = anchor.el && anchor.el.isConnected && anchor.el.parentElement === host ? anchor.el : null;
        // The anchor node may have been remounted with the same identity
        // (a transcript re-projection): data-testid carries the message id,
        // so match it before falling back to the stored child index.
        if (!el && anchor.testId) {
            for (let i = 0; i < kids.length; i += 1) {
                if (kids[i].getAttribute("data-testid") === anchor.testId) {
                    el = kids[i];
                    break;
                }
            }
        }
        if (!el && kids.length > 0) el = kids[Math.min(anchor.index, kids.length - 1)];
        if (!(el instanceof HTMLElement)) return;
        // Refresh the anchor onto the resolved element even when no drift is
        // measured: after a remount the stored node is dead, and leaving it in
        // place forces captureAnchor into a full rescan on every commit.
        anchorRef.current = { el, relTop: anchor.relTop, index: childIndexOf(host, el), testId: el.getAttribute("data-testid") };
        const drift = el.getBoundingClientRect().top - (container.getBoundingClientRect().top + anchor.relTop);
        if (Math.abs(drift) <= 1) return;
        programmaticScrollRef.current = true;
        try {
            container.scrollTop += drift;
        } finally {
            programmaticScrollRef.current = false;
        }
        // Snapshot channel: restores can fire per commit during a pathological
        // re-projection and must not eat the shared transition-event budget.
        logAIScrollSnapshot("anchor-restore", { ...containerMetrics(), drift: Math.round(drift) }, 1000);
    }, [anchorHost, containerMetrics]);

    const applyScroll = useCallback((behavior: ScrollBehavior) => {
        if (suspendedRef?.current || outputHostHidden(outputContainerRef.current)) return;
        programmaticScrollRef.current = true;
        try {
            const result = tryPinAssistantOutput(outputContainerRef.current, outputEndRef.current, behavior, userIntentRef.current);
            if (result === "abandoned") {
                commitScrolledUp(true);
                logAIScrollEvent("pin-abandoned", { ...containerMetrics(), intent: 1 });
            } else if (!userScrolledUpRef.current) {
                userIntentRef.current = false;
            }
            logAIScrollSnapshot("pin", { ...containerMetrics(), result, latch: userScrolledUpRef.current ? 1 : 0 });
        } finally {
            programmaticScrollRef.current = false;
        }
    }, [commitScrolledUp, containerMetrics, suspendedRef]);

    const scrollToBottom = useCallback((behavior: ScrollBehavior = "auto", force = false, settleFrames = 0) => {
        // A forced pin owns the follow latch even while the transcript is
        // covered or hidden: the uncover transition must resume following
        // instead of inheriting the scrolled-up state of the surface left.
        const wasLatched = userScrolledUpRef.current;
        if (force) {
            commitScrolledUp(false);
            userIntentRef.current = false;
        }
        if (suspendedRef?.current || outputHostHidden(outputContainerRef.current)) return;
        if (force) {
            if (wasLatched) logAIScrollEvent("force-repin", { ...containerMetrics() });
        } else if (userScrolledUpRef.current) {
            // The pin request will be dropped by runPinnedScroll; this is the
            // stuck state the diagnostics are hunting for.
            logAIScrollSnapshot("skip-latch", { ...containerMetrics(), settleFrames }, 5000);
        }
        runPinnedScroll(applyScroll, cancelScheduledScroll, (id) => { scrollRafRef.current = id; }, behavior, force, userScrolledUpRef.current, settleFrames);
    }, [applyScroll, cancelScheduledScroll, commitScrolledUp, containerMetrics, suspendedRef]);

    // Park the reader on a chosen user row. The follow latch must be set in
    // the same turn: a jump that leaves the tail would otherwise be pinned
    // back down by the next token flush. Landing already near the bottom
    // stays unlatched so the live reply can keep following.
    const jumpToUserTurn = useCallback((messageId: string): boolean => {
        const container = outputContainerRef.current;
        const id = messageId.trim();
        if (!container || !id || outputHostHidden(container)) return false;
        const node = container.querySelector(`[data-testid="assistant-chat-user-${locatorCssEscape(id)}"]`);
        if (!(node instanceof HTMLElement)) return false;
        const top = userTurnScrollTop(node.getBoundingClientRect().top, container.getBoundingClientRect().top, container.scrollTop);
        programmaticScrollRef.current = true;
        try {
            container.scrollTop = top;
        } finally {
            programmaticScrollRef.current = false;
        }
        const host = anchorHost(container);
        const anchorEl = transcriptAnchorElement(host, node);
        anchorRef.current = {
            el: anchorEl,
            relTop: anchorEl.getBoundingClientRect().top - container.getBoundingClientRect().top,
            index: childIndexOf(host, anchorEl),
            testId: anchorEl.getAttribute("data-testid"),
        };
        const away = isAwayFromBottom(container);
        userIntentRef.current = away;
        commitScrolledUp(away);
        return true;
    }, [anchorHost, commitScrolledUp]);

    // Scroll anchoring for a parked reader, checked after every commit before
    // paint. When the transcript re-projection moves the anchor row (or swaps
    // its node), put the viewport back on it — the alternative is the jump
    // to scrollTop ~0 seen in the diagnostics. A conversation switch is gated
    // here as well as in the follow effect: this layout phase runs first, and
    // the stored child index must never be applied to the next conversation's
    // rows.
    const anchorConversationKeyRef = useRef(conversationKey);
    useLayoutEffect(() => {
        if (conversationKey !== anchorConversationKeyRef.current) {
            anchorConversationKeyRef.current = conversationKey;
            anchorRef.current = null;
        }
        if (!userScrolledUpRef.current) {
            anchorRef.current = null;
            return;
        }
        if (!suspendedRef?.current) restoreAnchor();
        captureAnchor();
    });

    // The cover flag is written during render, after this hook is called, so it
    // cannot be a dependency. Compare it after each commit. Tokens that arrived
    // under the cover did not move scrollTop; pin once on the way back.
    useLayoutEffect(() => {
        const covered = !!suspendedRef?.current;
        const wasCovered = wasCoveredRef.current;
        wasCoveredRef.current = covered;
        if (!wasCovered || covered || userScrolledUpRef.current) return;
        const container = outputContainerRef.current;
        if (container) {
            container.querySelectorAll("[data-nested-scroll]").forEach((node) => {
                if (node instanceof HTMLElement) node.scrollTop = node.scrollHeight;
            });
        }
        scrollToBottom("auto");
    });

    useEffect(() => cancelScheduledScroll, [cancelScheduledScroll]);
    useEffect(() => {
        if (typeof ResizeObserver !== "function") return;
        // Follow content growth that does not change the messages array —
        // image/thumbnail decode, KaTeX or highlight upgrades, viewport
        // resizes. The follow effect re-points observation at newly rendered
        // children, because the conversation renders as a fragment: message
        // bubbles are direct children of the container and arrive over time,
        // so mount-time observation alone would miss nearly everything.
        const observer = new ResizeObserver(() => {
            // Coalesce a burst of resize notifications into one pin per frame.
            if (suspendedRef?.current || outputHostHidden(outputContainerRef.current) || userScrolledUpRef.current || resizeRafRef.current !== null || typeof requestAnimationFrame !== "function") return;
            resizeRafRef.current = requestAnimationFrame(() => {
                resizeRafRef.current = null;
                if (!userScrolledUpRef.current) applyScroll("auto");
            });
        });
        resizeObserverRef.current = observer;
        return () => {
            observer.disconnect();
            resizeObserverRef.current = null;
            if (resizeRafRef.current !== null && typeof cancelAnimationFrame === "function") {
                cancelAnimationFrame(resizeRafRef.current);
                resizeRafRef.current = null;
            }
        };
    }, [applyScroll, suspendedRef]);
    useEffect(() => {
        commitScrolledUp(false);
        userIntentRef.current = false;
        scrollToBottom("auto");
    }, [commitScrolledUp, scrollToBottom]);
    useEffect(() => {
        // Keep the resize observer pointed at the viewport and the current
        // tail bubble. observe() on an already-observed element is a no-op,
        // so older bubbles stay covered (late image decode anywhere in the
        // conversation still re-pins while the follow latch is clear).
        const container = outputContainerRef.current;
        const resizeObserver = resizeObserverRef.current;
        const tailMessage = messages[messages.length - 1];
        const tailStamp = `${tailMessage?.id ?? ""}:${tailMessage?.content?.length ?? 0}`;
        const tailUnchanged = tailStamp === prevTailStampRef.current;
        prevTailStampRef.current = tailStamp;
        if (container && resizeObserver) {
            resizeObserver.observe(container);
            const tail = container.lastElementChild?.previousElementSibling ?? container.lastElementChild;
            if (tail) resizeObserver.observe(tail);
        }
        // A freshly sent user message always re-pins the tail and clears the
        // scrolled-up latch: sending starts a new round, and without this the
        // whole reply streams below the fold whenever the user had scrolled
        // up during the previous round (only a manual scroll to the bottom or
        // a remount — e.g. switching tabs — would recover the follow).
        //
        // Detect it by the identity of the last user message, not by the tail
        // role: the send paths append the user message together with the
        // assistant placeholder, so the last message is usually an assistant
        // one even right after sending. Host-initiated rows (goal and
        // session continuations, remote-control or ACP turns) are excluded:
        // the reader did not send them, so a new backend round must not yank
        // a scrolled-up view back to the tail.
        const firstMessageId = messages[0]?.id;
        const tailMessageId = tailMessage?.id;
        const previousShape = prevConversationShapeRef.current;
        if (conversationKey !== previousShape.key) {
            // A different conversation owns the scroller now: neither the
            // parked latch nor DOM anchors from the previous transcript
            // apply to it.
            commitScrolledUp(false);
            userIntentRef.current = false;
            anchorRef.current = null;
        }
        const conversationChanged = conversationKey !== previousShape.key || messages.length !== prevMsgCountRef.current
            || firstMessageId !== previousShape.firstId || tailMessageId !== previousShape.tailId;
        let lastUserMsgId = prevLastUserMsgIdRef.current;
        if (conversationChanged) lastUserMsgId = messages.findLast(message => message.role === "user" && !message.hostInitiated)?.id;
        const appendedUserMessage = lastUserMsgId !== undefined && lastUserMsgId !== prevLastUserMsgIdRef.current;
        prevConversationShapeRef.current = { key: conversationKey, firstId: firstMessageId, tailId: tailMessageId };
        prevLastUserMsgIdRef.current = lastUserMsgId;
        // An empty baseline (latched before any transcript existed) still
        // counts: the first content to arrive is unread to that reader.
        if (userScrolledUpRef.current && latchedTailStampRef.current !== tailStamp) {
            setUnreadTail(true);
        }
        if (suspendedRef?.current || outputHostHidden(container)) {
            prevActivityKeyRef.current = activityKey;
            prevMsgCountRef.current = messages.length;
            return;
        }
        // Reasoning tokens keep the same reply body. Once the thought pane
        // was already capped, more reasoning does not move the outer
        // transcript, and reading its scrollHeight lays out every bubble.
        // The flush that first fills the pane still grows the outer box.
        const activityChanged = !!activityKey && activityKey !== prevActivityKeyRef.current;
        const reasoningOnly = tailUnchanged && !activityChanged && !appendedUserMessage;
        if (reasoningOnly) {
            const thoughtCapped = cappedThoughtAbsorbsGrowth(container);
            const thoughtAlreadyCapped = thoughtPaneCappedRef.current;
            thoughtPaneCappedRef.current = thoughtCapped;
            if (thoughtAlreadyCapped && thoughtCapped) {
                prevActivityKeyRef.current = activityKey;
                prevMsgCountRef.current = messages.length;
                return;
            }
        }
        if (appendedUserMessage) {
            logAIScrollEvent("new-round", { ...containerMetrics(), latch: userScrolledUpRef.current ? 1 : 0, msgs: messages.length });
        }
        const plan = planConversationFollow({
            activityKey,
            prevActivityKey: prevActivityKeyRef.current,
            userScrolledUp: userScrolledUpRef.current,
            messageCount: messages.length,
            prevMessageCount: prevMsgCountRef.current,
        });
        prevActivityKeyRef.current = plan.nextActivityKey;
        prevMsgCountRef.current = plan.nextMessageCount;
        if (appendedUserMessage) {
            scrollToBottom("auto", true, 2);
            return;
        }
        if (plan.settleFrames !== undefined) scrollToBottom("auto", false, plan.settleFrames);
        if (scrollSnapshotDue("follow", 1000)) {
            const tailMsg = messages[messages.length - 1];
            logAIScrollSnapshot("follow", {
                ...containerMetrics(),
                msgs: messages.length,
                tailRole: tailMsg?.role ?? "",
                tailLen: tailMsg?.content?.length ?? -1,
                latch: userScrolledUpRef.current ? 1 : 0,
            }, 1000);
        }
    }, [activityKey, commitScrolledUp, conversationKey, messages, scrollToBottom, containerMetrics, suspendedRef]);

    // Touch events carry no wheel delta; keep the previous touch Y so a
    // drag direction can be derived per touchmove and fed to the shared
    // nested-pane boundary rule.
    const lastTouchClientYRef = useRef<number | null>(null);
    const handleUserScrollIntent = useCallback((event?: AssistantScrollIntentEvent) => {
        let effective = event;
        if (event?.type === "touchmove") {
            const y = event.touches?.[0]?.clientY;
            if (typeof y === "number") {
                if (lastTouchClientYRef.current !== null) {
                    // A downward finger pull drags the content up — the
                    // negative delta matches the wheel-up sign convention.
                    effective = { type: event.type, target: event.target, currentTarget: event.currentTarget, deltaY: lastTouchClientYRef.current - y };
                }
                lastTouchClientYRef.current = y;
            }
        } else {
            // Any other gesture starts a new contact sequence; the next
            // touchmove measures from a clean baseline.
            lastTouchClientYRef.current = null;
        }
        if (!shouldIgnoreUserScrollIntent(effective)) userIntentRef.current = true;
    }, []);
    const handleScroll = useCallback(() => {
        if (programmaticScrollRef.current || !outputContainerRef.current || outputHostHidden(outputContainerRef.current)) return;
        const next = applyUserScrollFollow(userIntentRef.current, isAwayFromBottom(outputContainerRef.current), userScrolledUpRef.current);
        if (next.userScrolledUp !== userScrolledUpRef.current || next.userIntent !== userIntentRef.current) {
            logAIScrollEvent("latch", {
                ...containerMetrics(),
                latch: next.userScrolledUp ? 1 : 0,
                intent: next.userIntent ? 1 : 0,
            });
        }
        if (next.userScrolledUp !== userScrolledUpRef.current) commitScrolledUp(next.userScrolledUp);
        userIntentRef.current = next.userIntent;
        // Scrolls between commits move the anchor row too; refresh it so the
        // next restore does not snap back to a stale position.
        captureAnchor();
    }, [captureAnchor, commitScrolledUp, containerMetrics]);

    // The reported recovery path is switching tabs; log the visibility
    // transition together with layout metrics so we can correlate it.
    useEffect(() => {
        if (typeof document === "undefined" || typeof document.addEventListener !== "function") return;
        const onVisibility = () => logAIScrollEvent("visibility", { ...containerMetrics(), latch: userScrolledUpRef.current ? 1 : 0 });
        document.addEventListener("visibilitychange", onVisibility);
        return () => document.removeEventListener("visibilitychange", onVisibility);
    }, [containerMetrics]);

    useEffect(() => {
        const becameReady = !prevReadyRef.current && ready;
        prevReadyRef.current = ready;
        if (becameReady && !userScrolledUpRef.current && hasConversation) scrollToBottom("auto");
    }, [ready, hasConversation, scrollToBottom]);
    useEffect(() => {
        if (!scrollToTopSeq || hasConversation || !outputContainerRef.current) return;
        programmaticScrollRef.current = true;
        try { pinAssistantOutputToTop(outputContainerRef.current); } finally { programmaticScrollRef.current = false; }
        // The smooth top-pin repositions the view itself; the stale anchor
        // row must not be dragged back into place mid-animation. Scroll
        // events during the animation recapture the anchor at each frame.
        anchorRef.current = null;
        userIntentRef.current = true;
        commitScrolledUp(true);
    }, [commitScrolledUp, scrollToTopSeq, hasConversation]);

    return { clearScrolledUp, handleScroll, handleUserScrollIntent, jumpToUserTurn, outputContainerRef, outputEndRef, scrolledUp, scrollToBottom, unreadTail, userScrolledUpRef };
}
