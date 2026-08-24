import { useCallback, useEffect, useLayoutEffect, useRef } from "react";
import {
    ASSISTANT_REASONING_NEAR_BOTTOM_PX,
    applyUserScrollFollow,
    isAwayFromBottom,
    runPinnedScroll,
    shouldIgnoreNestedScrollIntent,
    tryPinNestedScroll,
    type AssistantScrollIntentEvent,
} from "./assistantOutputScrollLogic";

export function useNestedPinnedScroll(active: boolean, contentKey: string) {
    const bodyRef = useRef<HTMLDivElement | null>(null);
    const contentRef = useRef<HTMLDivElement | null>(null);
    const pinnedRef = useRef(true);
    const userIntentRef = useRef(false);
    const programmaticRef = useRef(false);
    const rafRef = useRef<number | null>(null);

    const applyScroll = useCallback((_behavior?: ScrollBehavior) => {
        const el = bodyRef.current;
        if (!el || !active) return;
        programmaticRef.current = true;
        try {
            if (tryPinNestedScroll(el, pinnedRef.current, userIntentRef.current) === "abandoned") {
                pinnedRef.current = false;
            }
        } finally {
            programmaticRef.current = false;
        }
    }, [active]);

    const cancelScheduled = useCallback(() => {
        if (rafRef.current == null || typeof cancelAnimationFrame !== "function") return;
        cancelAnimationFrame(rafRef.current);
        rafRef.current = null;
    }, []);

    const pinToBottom = useCallback((settleFrames = 1) => {
        const el = bodyRef.current;
        if (!el || !pinnedRef.current) return;
        if (userIntentRef.current && isAwayFromBottom(el, ASSISTANT_REASONING_NEAR_BOTTOM_PX)) {
            pinnedRef.current = false;
            cancelScheduled();
            return;
        }
        runPinnedScroll(
            applyScroll,
            cancelScheduled,
            (id) => { rafRef.current = id; },
            "auto",
            false,
            false,
            settleFrames,
        );
    }, [applyScroll, cancelScheduled]);

    useLayoutEffect(() => {
        if (!active) {
            pinnedRef.current = true;
            userIntentRef.current = false;
            return;
        }
        pinToBottom(1);
        return cancelScheduled;
    }, [active, contentKey, pinToBottom, cancelScheduled]);

    useEffect(() => {
        const content = contentRef.current;
        if (!content || !active || typeof ResizeObserver === "undefined") return;
        const observer = new ResizeObserver(() => {
            if (pinnedRef.current) applyScroll();
        });
        observer.observe(content);
        return () => observer.disconnect();
    }, [active, applyScroll]);

    const handleUserScrollIntent = useCallback((event?: AssistantScrollIntentEvent) => {
        if (!shouldIgnoreNestedScrollIntent(event)) userIntentRef.current = true;
    }, []);

    const handleScroll = useCallback(() => {
        const el = bodyRef.current;
        if (programmaticRef.current || !el) return;
        const next = applyUserScrollFollow(
            userIntentRef.current,
            isAwayFromBottom(el, ASSISTANT_REASONING_NEAR_BOTTOM_PX),
            !pinnedRef.current,
        );
        pinnedRef.current = !next.userScrolledUp;
        userIntentRef.current = next.userIntent;
    }, []);

    return { bodyRef, contentRef, handleScroll, handleUserScrollIntent };
}
