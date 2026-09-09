import { useCallback, useEffect, useRef } from "react";
import type { ChatMessage } from "./useAIAssistant";
import { logAIScrollEvent, logAIScrollSnapshot } from "./assistantScrollDiag";
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

export { ASSISTANT_OUTPUT_NEAR_BOTTOM_PX };

interface AssistantOutputScrollOptions {
    activityKey?: string;
    hasConversation: boolean;
    messages: ChatMessage[];
    ready: boolean;
    scrollToTopSeq?: number;
}

export function useAssistantOutputScroll({ activityKey, hasConversation, messages, ready, scrollToTopSeq }: AssistantOutputScrollOptions) {
    const outputEndRef = useRef<HTMLDivElement | null>(null);
    const outputContainerRef = useRef<HTMLDivElement | null>(null);
    const userScrolledUpRef = useRef(false);
    const userIntentRef = useRef(false);
    const prevMsgCountRef = useRef(0);
    const prevLastUserMsgIdRef = useRef<string | undefined>(undefined);
    const prevActivityKeyRef = useRef(activityKey);
    const scrollRafRef = useRef<number | null>(null);
    const prevReadyRef = useRef(ready);
    const programmaticScrollRef = useRef(false);
    const resizeObserverRef = useRef<ResizeObserver | null>(null);
    const resizeRafRef = useRef<number | null>(null);

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

    const applyScroll = useCallback((behavior: ScrollBehavior) => {
        programmaticScrollRef.current = true;
        try {
            const result = tryPinAssistantOutput(outputContainerRef.current, outputEndRef.current, behavior, userIntentRef.current);
            if (result === "abandoned") {
                userScrolledUpRef.current = true;
                logAIScrollEvent("pin-abandoned", { ...containerMetrics(), intent: 1 });
            } else if (!userScrolledUpRef.current) {
                userIntentRef.current = false;
            }
            logAIScrollSnapshot("pin", { ...containerMetrics(), result, latch: userScrolledUpRef.current ? 1 : 0 });
        } finally {
            programmaticScrollRef.current = false;
        }
    }, [containerMetrics]);

    const scrollToBottom = useCallback((behavior: ScrollBehavior = "auto", force = false, settleFrames = 0) => {
        if (force) {
            if (userScrolledUpRef.current) logAIScrollEvent("force-repin", { ...containerMetrics() });
            userScrolledUpRef.current = false;
            userIntentRef.current = false;
        } else if (userScrolledUpRef.current) {
            // The pin request will be dropped by runPinnedScroll; this is the
            // stuck state the diagnostics are hunting for.
            logAIScrollSnapshot("skip-latch", { ...containerMetrics(), settleFrames }, 5000);
        }
        runPinnedScroll(applyScroll, cancelScheduledScroll, (id) => { scrollRafRef.current = id; }, behavior, force, userScrolledUpRef.current, settleFrames);
    }, [applyScroll, cancelScheduledScroll, containerMetrics]);

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
            if (userScrolledUpRef.current || resizeRafRef.current !== null || typeof requestAnimationFrame !== "function") return;
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
    }, [applyScroll]);
    useEffect(() => {
        userScrolledUpRef.current = false;
        userIntentRef.current = false;
        scrollToBottom("auto");
    }, [scrollToBottom]);
    useEffect(() => {
        // Keep the resize observer pointed at the viewport and the current
        // tail bubble. observe() on an already-observed element is a no-op,
        // so older bubbles stay covered (late image decode anywhere in the
        // conversation still re-pins while the follow latch is clear).
        const container = outputContainerRef.current;
        const resizeObserver = resizeObserverRef.current;
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
        // one even right after sending.
        let lastUserMsgId: string | undefined;
        for (let i = messages.length - 1; i >= 0; i--) {
            if (messages[i]?.role === "user") {
                lastUserMsgId = messages[i].id;
                break;
            }
        }
        const appendedUserMessage = lastUserMsgId !== undefined && lastUserMsgId !== prevLastUserMsgIdRef.current;
        prevLastUserMsgIdRef.current = lastUserMsgId;
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
        const tailMsg = messages[messages.length - 1];
        logAIScrollSnapshot("follow", {
            ...containerMetrics(),
            msgs: messages.length,
            tailRole: tailMsg?.role ?? "",
            tailLen: tailMsg?.content?.length ?? -1,
            latch: userScrolledUpRef.current ? 1 : 0,
        }, 1000);
    }, [activityKey, messages, scrollToBottom, containerMetrics]);

    const handleUserScrollIntent = useCallback((event?: AssistantScrollIntentEvent) => {
        if (!shouldIgnoreUserScrollIntent(event)) userIntentRef.current = true;
    }, []);
    const handleScroll = useCallback(() => {
        if (programmaticScrollRef.current || !outputContainerRef.current) return;
        const next = applyUserScrollFollow(userIntentRef.current, isAwayFromBottom(outputContainerRef.current), userScrolledUpRef.current);
        if (next.userScrolledUp !== userScrolledUpRef.current || next.userIntent !== userIntentRef.current) {
            logAIScrollEvent("latch", {
                ...containerMetrics(),
                latch: next.userScrolledUp ? 1 : 0,
                intent: next.userIntent ? 1 : 0,
            });
        }
        userScrolledUpRef.current = next.userScrolledUp;
        userIntentRef.current = next.userIntent;
    }, [containerMetrics]);

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
        userIntentRef.current = true;
        userScrolledUpRef.current = true;
    }, [scrollToTopSeq, hasConversation]);

    return { handleScroll, handleUserScrollIntent, outputContainerRef, outputEndRef, scrollToBottom, userScrolledUpRef };
}
