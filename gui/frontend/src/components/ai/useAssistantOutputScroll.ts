import { useCallback, useEffect, useRef } from "react";
import type { ChatMessage } from "./useAIAssistant";

interface AssistantOutputScrollOptions {
    hasConversation: boolean;
    messages: ChatMessage[];
    ready: boolean;
    scrollToTopSeq?: number;
}

export function useAssistantOutputScroll({ hasConversation, messages, ready, scrollToTopSeq }: AssistantOutputScrollOptions) {
    const outputEndRef = useRef<HTMLDivElement | null>(null);
    const outputContainerRef = useRef<HTMLDivElement | null>(null);
    const userScrolledUpRef = useRef(false);
    const prevMsgCountRef = useRef(0);
    const scrollTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const prevReadyRef = useRef(ready);

    useEffect(() => {
        userScrolledUpRef.current = false;
        outputEndRef.current?.scrollIntoView({ behavior: "auto" });
    }, []);

    useEffect(() => {
        if (userScrolledUpRef.current) {
            prevMsgCountRef.current = messages.length;
            return;
        }
        if (messages.length !== prevMsgCountRef.current) {
            prevMsgCountRef.current = messages.length;
            outputEndRef.current?.scrollIntoView({ behavior: "smooth" });
            return;
        }
        if (scrollTimerRef.current) clearTimeout(scrollTimerRef.current);
        scrollTimerRef.current = setTimeout(() => {
            outputEndRef.current?.scrollIntoView({ behavior: "auto" });
        }, 80);
        return () => {
            if (scrollTimerRef.current) clearTimeout(scrollTimerRef.current);
        };
    }, [messages]);

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
        outputEndRef.current?.scrollIntoView({ behavior: "auto" });
    }, [ready, hasConversation]);

    useEffect(() => {
        if (!scrollToTopSeq || hasConversation) return;
        const container = outputContainerRef.current;
        if (container) {
            container.scrollTo({ top: 0, behavior: "smooth" });
            userScrolledUpRef.current = true;
        }
    }, [scrollToTopSeq, hasConversation]);

    return { handleScroll, outputContainerRef, outputEndRef, userScrolledUpRef };
}
