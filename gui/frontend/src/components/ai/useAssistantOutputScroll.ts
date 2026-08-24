import { useCallback, useEffect, useRef } from "react";
import type { ChatMessage } from "./useAIAssistant";
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
    const prevActivityKeyRef = useRef(activityKey);
    const scrollRafRef = useRef<number | null>(null);
    const prevReadyRef = useRef(ready);
    const programmaticScrollRef = useRef(false);

    const cancelScheduledScroll = useCallback(() => {
        if (scrollRafRef.current === null || typeof cancelAnimationFrame !== "function") return;
        cancelAnimationFrame(scrollRafRef.current);
        scrollRafRef.current = null;
    }, []);

    const applyScroll = useCallback((behavior: ScrollBehavior) => {
        programmaticScrollRef.current = true;
        try {
            if (tryPinAssistantOutput(outputContainerRef.current, outputEndRef.current, behavior, userIntentRef.current) === "abandoned") {
                userScrolledUpRef.current = true;
            } else if (!userScrolledUpRef.current) {
                userIntentRef.current = false;
            }
        } finally {
            programmaticScrollRef.current = false;
        }
    }, []);

    const scrollToBottom = useCallback((behavior: ScrollBehavior = "auto", force = false, settleFrames = 0) => {
        if (force) {
            userScrolledUpRef.current = false;
            userIntentRef.current = false;
        }
        runPinnedScroll(applyScroll, cancelScheduledScroll, (id) => { scrollRafRef.current = id; }, behavior, force, userScrolledUpRef.current, settleFrames);
    }, [applyScroll, cancelScheduledScroll]);

    useEffect(() => cancelScheduledScroll, [cancelScheduledScroll]);
    useEffect(() => {
        userScrolledUpRef.current = false;
        userIntentRef.current = false;
        scrollToBottom("auto");
    }, [scrollToBottom]);
    useEffect(() => {
        const plan = planConversationFollow({
            activityKey,
            prevActivityKey: prevActivityKeyRef.current,
            userScrolledUp: userScrolledUpRef.current,
            messageCount: messages.length,
            prevMessageCount: prevMsgCountRef.current,
        });
        prevActivityKeyRef.current = plan.nextActivityKey;
        prevMsgCountRef.current = plan.nextMessageCount;
        if (plan.settleFrames !== undefined) scrollToBottom("auto", false, plan.settleFrames);
    }, [activityKey, messages, scrollToBottom]);

    const handleUserScrollIntent = useCallback((event?: AssistantScrollIntentEvent) => {
        if (!shouldIgnoreUserScrollIntent(event)) userIntentRef.current = true;
    }, []);
    const handleScroll = useCallback(() => {
        if (programmaticScrollRef.current || !outputContainerRef.current) return;
        const next = applyUserScrollFollow(userIntentRef.current, isAwayFromBottom(outputContainerRef.current), userScrolledUpRef.current);
        userScrolledUpRef.current = next.userScrolledUp;
        userIntentRef.current = next.userIntent;
    }, []);

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
