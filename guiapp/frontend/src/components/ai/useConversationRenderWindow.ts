import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import {
    CONVERSATION_RENDER_BLANK_PX,
    conversationRenderCanWindow,
    conversationRenderLoadStart,
    conversationRenderTrimStart,
    conversationRenderTurnStart,
    type ConversationRenderWindowMessage,
} from "./conversationRenderWindow";

/**
 * Keeps only the last screens of the live transcript in the DOM.
 *
 * The message array is untouched — the caller passes the whole hot window and
 * renders `messages.slice(windowStart)`. Everything above is replaced by one
 * spacer of exactly the removed height, so scrollbar geometry, the scroll
 * position and the session locator keep working.
 *
 * Heights are not measured per row: a trim/load commit is followed by a second
 * commit that corrects the spacer from the measured scrollHeight delta. Layout
 * effects cascade before paint, so neither commit is visible.
 *
 * Measurement caveats, both self-healing:
 * - remounted rows sit offscreen at the 96px `content-visibility` placeholder
 *   until they render, so a load correction can under-subtract the spacer
 *   (cleared by the blank-area branch and by the nothing-unmounted invariant);
 * - a trim measures rows that were rendered at least once, so their remembered
 *   size keeps the delta accurate; when it is not, the spacer under-counts and
 *   the next blank-area entry restores it.
 */
interface ConversationRenderWindowOptions<T extends ConversationRenderWindowMessage> {
    containerRef: { readonly current: HTMLDivElement | null };
    messages: T[];
    /** Reset identity: a different conversation never reuses the old window. */
    conversationKey: string;
    /** Live latch from useAssistantOutputScroll: true means the reader is parked away from the tail. */
    scrolledUpRef: { readonly current: boolean };
    /** Same cover flag the scroll hook uses: a covered transcript is not measured. */
    suspendedRef?: { readonly current: boolean };
    enabled: boolean;
}

/**
 * One window change is followed by a second commit that reconciles the spacer
 * with the measured scrollHeight delta, then (for trims) re-pins the bottom.
 */
type SpacerCorrection =
    | { kind: "trim" | "load"; contentPx: number }
    | { kind: "trim-pin" };

export interface ConversationRenderWindow<T extends ConversationRenderWindowMessage> {
    /** First message index that owns DOM nodes; always 0 or a user turn start. */
    windowStart: number;
    /** Pixels of unmounted transcript above the window. Rendered as one spacer div. */
    spacerPx: number;
    spacerRef: React.MutableRefObject<HTMLDivElement | null>;
    /**
     * Expand the window so `id` is mounted. Returns false when the row is
     * already mounted (or unknown), so the caller can fall back to the archive.
     */
    revealMessage: (id: string) => boolean;
}

function indexOfRow<T extends { id: string }>(messages: T[], id: string | null): number {
    if (!id) return 0;
    return messages.findIndex((message) => message.id === id);
}

export function useConversationRenderWindow<T extends ConversationRenderWindowMessage>({
    containerRef,
    messages,
    conversationKey,
    scrolledUpRef,
    suspendedRef,
    enabled,
}: ConversationRenderWindowOptions<T>): ConversationRenderWindow<T> {
    const spacerRef = useRef<HTMLDivElement | null>(null);
    const [startId, setStartId] = useState<string | null>(null);
    const [spacerPx, setSpacerPx] = useState(0);
    const pendingRef = useRef<SpacerCorrection | null>(null);
    const messagesRef = useRef(messages);
    messagesRef.current = messages;
    const startIdRef = useRef<string | null>(startId);
    startIdRef.current = startId;
    const spacerPxRef = useRef(spacerPx);
    spacerPxRef.current = spacerPx;
    const enabledRef = useRef(enabled);
    enabledRef.current = enabled;
    const bodyCacheRef = useRef<{ container: HTMLElement; body: Element } | null>(null);
    // A locator jump reveals a row that then waits one effect pass before the
    // scroller parks on it. Trimming in that gap would unmount it again.
    const trimHoldUntilRef = useRef(0);

    // Before paint: a switched conversation must never inherit the previous
    // spacer height for a frame.
    useLayoutEffect(() => {
        pendingRef.current = null;
        setStartId(null);
        setSpacerPx(0);
    }, [conversationKey]);

    const windowStart = useMemo(() => {
        const index = indexOfRow(messages, startId);
        return index < 0 ? 0 : index;
    }, [messages, startId]);

    // A fold into the archive can drop the window's first row. Fall back to the
    // start of what is left rather than slicing an index that moved.
    useLayoutEffect(() => {
        if (!startId) return;
        if (indexOfRow(messages, startId) >= 0) return;
        pendingRef.current = null;
        setStartId(null);
        setSpacerPx(0);
    }, [messages, startId]);

    const bodyOf = useCallback((container: HTMLElement): Element | null => {
        const cached = bodyCacheRef.current;
        if (cached && cached.container === container && cached.body.isConnected) return cached.body;
        const body = container.querySelector(".mc-conversation-body");
        bodyCacheRef.current = body ? { container, body } : null;
        return body;
    }, []);

    const evaluate = useCallback(() => {
        if (!enabledRef.current || suspendedRef?.current) return;
        const container = containerRef.current;
        if (!container || !spacerRef.current) return;
        const viewportPx = container.clientHeight;
        if (!conversationRenderCanWindow(viewportPx)) return;
        const body = bodyOf(container);
        if (!body) return;
        const list = messagesRef.current;
        const currentStart = indexOfRow(list, startIdRef.current);
        if (currentStart < 0) return;
        if (currentStart === 0 && spacerPxRef.current > 0) {
            // Invariant: nothing unmounted means no spacer. Load corrections
            // under-measure because remounted rows sit offscreen at the 96px
            // content-visibility placeholder until they render, so the spacer
            // can drift upward; left alone it becomes a phantom blank band at
            // the very top of the transcript with nothing left to fill it.
            setSpacerPx(0);
            return;
        }
        const scrollHeight = container.scrollHeight;
        const contentPx = Math.max(0, scrollHeight - spacerPxRef.current);

        if (!scrolledUpRef.current) {
            if (Date.now() < trimHoldUntilRef.current) return;
            const next = conversationRenderTrimStart({ messages: list, windowStart: currentStart, viewportPx, contentPx });
            if (next === null) return;
            pendingRef.current = { kind: "trim", contentPx: scrollHeight };
            setStartId(list[next].id);
            return;
        }

        if (currentStart <= 0) return;
        const offset = body.getBoundingClientRect().top - container.getBoundingClientRect().top;
        // Blank means the viewport sits inside the spacer itself, so the spacer
        // must be positive: padding plus the archive banner can push the body
        // top past the threshold at scrollTop 0 without any blank area.
        if (offset > CONVERSATION_RENDER_BLANK_PX && spacerPxRef.current > 0) {
            // The reader dragged past everything that is mounted. Batches would
            // leave them staring at the spacer, so put the rest back at once.
            pendingRef.current = { kind: "load", contentPx: scrollHeight };
            setSpacerPx(0);
            setStartId(null);
            return;
        }
        if (offset <= -viewportPx) return;
        const next = conversationRenderLoadStart({ messages: list, windowStart: currentStart, viewportPx, contentPx });
        if (next === null) return;
        pendingRef.current = { kind: "load", contentPx: scrollHeight };
        setStartId(next === 0 ? null : list[next].id);
    }, [bodyOf, containerRef, scrolledUpRef, suspendedRef]);

    const evaluateRef = useRef(evaluate);
    evaluateRef.current = evaluate;

    // Runs on every commit: apply the pending spacer correction first (the
    // window change from the previous commit), then decide whether the window
    // still fits the budget. Both branches only read geometry.
    useLayoutEffect(() => {
        const container = containerRef.current;
        if (!container) return;
        const pending = pendingRef.current;
        if (pending) {
            pendingRef.current = null;
            const contentPx = container.scrollHeight;
            if (pending.kind === "trim") {
                const removed = pending.contentPx - contentPx;
                if (removed > 0) setSpacerPx((prev) => prev + removed);
                // The spacer setter re-commits; pin there so the bottom does not
                // drift while the height is restored.
                pendingRef.current = { kind: "trim-pin" };
                return;
            }
            if (pending.kind === "trim-pin") {
                if (!scrolledUpRef.current) container.scrollTop = container.scrollHeight;
                return;
            }
            const added = contentPx - pending.contentPx;
            if (added > 0) setSpacerPx((prev) => Math.max(0, prev - added));
            return;
        }
        evaluateRef.current();
    });

    useEffect(() => {
        if (!enabled) return;
        if (typeof requestAnimationFrame !== "function") return;
        const id = requestAnimationFrame(() => evaluateRef.current());
        return () => cancelAnimationFrame(id);
    }, [enabled, conversationKey, messages.length]);

    useEffect(() => {
        if (!enabled || typeof ResizeObserver !== "function") return;
        const container = containerRef.current;
        if (!container) return;
        let frame = 0;
        const observer = new ResizeObserver(() => {
            if (frame !== 0 || typeof requestAnimationFrame !== "function") return;
            frame = requestAnimationFrame(() => {
                frame = 0;
                evaluateRef.current();
            });
        });
        observer.observe(container);
        return () => {
            observer.disconnect();
            if (frame !== 0 && typeof cancelAnimationFrame === "function") cancelAnimationFrame(frame);
        };
    }, [containerRef, enabled, conversationKey]);

    const revealMessage = useCallback((id: string) => {
        const trimmed = id.trim();
        const list = messagesRef.current;
        if (!trimmed || list.length === 0) return false;
        const target = list.findIndex((message) => message.id === trimmed);
        if (target < 0) return false;
        const currentStart = indexOfRow(list, startIdRef.current);
        if (target >= currentStart) return false;
        const turnStart = conversationRenderTurnStart(list, target);
        pendingRef.current = null;
        trimHoldUntilRef.current = Date.now() + 1500;
        // The rows above the revealed turn stay unmounted. Scale their spacer
        // by the remaining row share (rows are roughly uniform) instead of
        // zeroing it, so the scrollbar still shows that history exists.
        const keptPx = currentStart > 0
            ? Math.round(spacerPxRef.current * (turnStart / currentStart))
            : 0;
        setStartId(turnStart === 0 ? null : list[turnStart].id);
        setSpacerPx(Math.max(0, keptPx));
        return true;
    }, []);

    return { windowStart, spacerPx, spacerRef, revealMessage };
}
