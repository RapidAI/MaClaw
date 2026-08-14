import { useCallback, useEffect, useRef } from "react";
import type { ChatMessage } from "./useAIAssistant";

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
    const prevMsgCountRef = useRef(0);
    const prevActivityKeyRef = useRef(activityKey);
    const scrollTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const scrollRafRef = useRef<number | null>(null);
    const prevReadyRef = useRef(ready);

    const cancelScheduledScroll = useCallback(() => {
        if (scrollRafRef.current === null || typeof cancelAnimationFrame !== "function") return;
        cancelAnimationFrame(scrollRafRef.current);
        scrollRafRef.current = null;
    }, []);

    const scrollToBottom = useCallback((behavior: ScrollBehavior = "auto", force = false, settleFrames = 0) => {
        if (force) userScrolledUpRef.current = false;
        const scroll = () => {
            // A follow-up frame is only to account for layout settling. If the
            // user starts reading older content in the meantime, do not pull
            // them back down. Explicitly forced scrolls (such as resizing the
            // input) retain their existing behavior.
            if (!force && userScrolledUpRef.current) return;
            outputEndRef.current?.scrollIntoView({ behavior });
        };
        const scheduleSettledScroll = (remainingFrames: number) => {
            if (remainingFrames <= 0 || typeof requestAnimationFrame !== "function") return;
            scrollRafRef.current = requestAnimationFrame(() => {
                scrollRafRef.current = null;
                scroll();
                scheduleSettledScroll(remainingFrames - 1);
            });
        };
        cancelScheduledScroll();
        scroll();
        scheduleSettledScroll(settleFrames);
    }, [cancelScheduledScroll]);

    useEffect(() => {
        return () => {
            if (scrollTimerRef.current) clearTimeout(scrollTimerRef.current);
            cancelScheduledScroll();
        };
    }, [cancelScheduledScroll]);

    useEffect(() => {
        userScrolledUpRef.current = false;
        scrollToBottom("auto");
    }, [scrollToBottom]);

    useEffect(() => {
        const activityChanged = !!activityKey && activityKey !== prevActivityKeyRef.current;
        prevActivityKeyRef.current = activityKey;
        if (userScrolledUpRef.current) {
            prevMsgCountRef.current = messages.length;
            return;
        }
        // System status/progress rows are rendered separately from messages.
        // Scroll them into view immediately, then once more after layout settles.
        if (activityChanged) {
            prevMsgCountRef.current = messages.length;
            scrollToBottom("auto", false, 1);
            return;
        }
        if (messages.length !== prevMsgCountRef.current) {
            prevMsgCountRef.current = messages.length;
            scrollToBottom("smooth");
            return;
        }
        if (scrollTimerRef.current) clearTimeout(scrollTimerRef.current);
        scrollTimerRef.current = setTimeout(() => {
            scrollToBottom("auto");
        }, 80);
        return () => {
            if (scrollTimerRef.current) clearTimeout(scrollTimerRef.current);
        };
    }, [activityKey, messages, scrollToBottom]);

    const handleScroll = useCallback(() => {
        const container = outputContainerRef.current;
        if (!container) return;
        const threshold = 80;
        userScrolledUpRef.current =
            container.scrollHeight - container.scrollTop - container.clientHeight > threshold;
    }, []);

    useEffect(() => {
        const becameReady = !prevReadyRef.current && ready;
        prevReadyRef.current = ready;
        if (!becameReady || userScrolledUpRef.current || !hasConversation) return;
        scrollToBottom("auto");
    }, [ready, hasConversation, scrollToBottom]);

    useEffect(() => {
        if (!scrollToTopSeq || hasConversation) return;
        const container = outputContainerRef.current;
        if (container) {
            container.scrollTo({ top: 0, behavior: "smooth" });
            userScrolledUpRef.current = true;
        }
    }, [scrollToTopSeq, hasConversation]);

    return { handleScroll, outputContainerRef, outputEndRef, scrollToBottom, userScrolledUpRef };
}
