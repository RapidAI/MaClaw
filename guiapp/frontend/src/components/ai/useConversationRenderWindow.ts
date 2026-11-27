import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import {
    CONVERSATION_RENDER_BLANK_PX,
    TRANSCRIPT_WINDOW_ADJUSTING_ATTR,
    cssPerVisualPx,
    conversationRenderCanWindow,
    conversationRenderLoadStart,
    conversationRenderTrimCeilingPx,
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
 * A trim measures the prefix that is about to unmount and grows the spacer in
 * the same commit, so scrollHeight never collapses for a frame. Anchor restore
 * reads that collapse as drift and parks the reader in the hole (white screen
 * on scroll-up). The measure is the direct children of those rows, in CSS
 * pixels. offsetTop is already CSS; rects are converted with
 * offsetHeight / visual height because `.app-scale-layer` scales them. The
 * scroll-up offset uses that same ratio: the blank threshold and the viewport
 * are CSS pixels. The margin that collapses
 * out of that border-box span is included, because unmounting the prefix
 * removes it. The scrollHeight-delta path remains only when a prefix row has
 * no geometry (jsdom, a row that is not in the DOM). While that path is in
 * flight the scroller carries TRANSCRIPT_WINDOW_ADJUSTING_ATTR and anchor
 * restore stands down.
 *
 * A load correction can still under-subtract the spacer when the newly mounted
 * rows have not taken their real height yet. The blank-area branch and the
 * nothing-unmounted invariant clear that leftover.
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
    | { kind: "trim"; contentPx: number; previousStartId: string | null }
    | { kind: "load"; contentPx: number }
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

/** Longest message id for which `testId` is one of that row's test ids. */
function longestOwnedId(testId: string, ids: readonly string[]): string {
    let best = "";
    for (let i = 0; i < ids.length; i += 1) {
        const id = ids[i];
        if (id.length <= best.length) continue;
        if (testId.endsWith(`-${id}`)) best = id;
    }
    return best;
}

function stylePx(style: CSSStyleDeclaration, edge: "marginTop" | "marginBottom"): number {
    const value = parseFloat(style[edge]);
    return Number.isFinite(value) ? value : 0;
}

/**
 * Margin that disappears with the prefix. Margins between the measured rows are
 * already inside the border-box span; the leading margin is not, and the
 * trailing margin only disappears by the part that exceeds the next sibling's
 * margin (the two collapse).
 */
function outsideMarginPx(first: HTMLElement, last: HTMLElement): number {
    const firstStyle = getComputedStyle(first);
    const lastStyle = first === last ? firstStyle : getComputedStyle(last);
    const leadMargin = stylePx(firstStyle, "marginTop");
    const tailMargin = stylePx(lastStyle, "marginBottom");
    const previous = first.previousElementSibling;
    let lead = leadMargin;
    if (previous instanceof HTMLElement) {
        const previousMargin = stylePx(getComputedStyle(previous), "marginBottom");
        lead = Math.max(previousMargin, leadMargin) - previousMargin;
    }
    const next = last.nextElementSibling;
    let tail = tailMargin;
    if (next instanceof HTMLElement) {
        const nextMargin = stylePx(getComputedStyle(next), "marginTop");
        tail = Math.max(tailMargin, nextMargin) - nextMargin;
    }
    return lead + tail;
}

/**
 * Border-box span of the prefix in CSS pixels. offsetTop is already CSS and
 * ignores the scale transform; rects are the fallback when the rows have no
 * layout parent (jsdom).
 */
function borderSpanCssPx(first: HTMLElement, last: HTMLElement): number {
    const sameParent = first.offsetParent !== null && first.offsetParent === last.offsetParent;
    if (sameParent && first.offsetHeight > 0 && last.offsetHeight > 0 && (first === last || last.offsetTop > first.offsetTop)) {
        const span = last.offsetTop + last.offsetHeight - first.offsetTop;
        if (span > 0) return span;
    }
    const visual = last.getBoundingClientRect().bottom - first.getBoundingClientRect().top;
    if (!(visual > 0)) return 0;
    return visual * cssPerVisualPx(first);
}

/**
 * CSS-pixel height of the prefix a trim is about to unmount, or 0 when a row
 * has no layout. 0 sends the caller down the scrollHeight-delta path.
 */
function measuredPrefixPx(body: Element, messages: ConversationRenderWindowMessage[], from: number, to: number): number {
    if (to <= from) return 0;
    const ids: string[] = [];
    for (let i = from; i < to; i += 1) {
        const id = messages[i]?.id;
        if (!id) return 0;
        ids.push(id);
    }
    const childOf = new Map<string, HTMLElement>();
    for (const child of body.children) {
        if (!(child instanceof HTMLElement)) continue;
        let owner = longestOwnedId(child.getAttribute("data-testid") || "", ids);
        // A child that already has its own testid is the row (or a sibling such
        // as the coding timeline). Only a testid-less wrap hides the row id on
        // a descendant, and only that wrap is worth walking.
        if (!owner && !child.hasAttribute("data-testid")) {
            const nodes = child.querySelectorAll("[data-testid]");
            for (const node of nodes) {
                const id = longestOwnedId(node.getAttribute("data-testid") || "", ids);
                if (id.length > owner.length) owner = id;
            }
        }
        if (owner && !childOf.has(owner)) childOf.set(owner, child);
    }
    const rows: HTMLElement[] = [];
    const seen = new Set<HTMLElement>();
    for (const id of ids) {
        const row = childOf.get(id);
        // A missing row, or two ids climbing to one element, would skip height.
        if (!row || seen.has(row)) return 0;
        if (!(row.offsetHeight > 0)) {
            const rect = row.getBoundingClientRect();
            if (!(rect.bottom > rect.top)) return 0;
        }
        seen.add(row);
        rows.push(row);
    }
    let first = rows[0];
    let last = rows[0];
    for (let i = 1; i < rows.length; i += 1) {
        const row = rows[i];
        if (first.compareDocumentPosition(row) & Node.DOCUMENT_POSITION_PRECEDING) first = row;
        if (last.compareDocumentPosition(row) & Node.DOCUMENT_POSITION_FOLLOWING) last = row;
    }
    const span = borderSpanCssPx(first, last);
    if (!(span > 0)) return 0;
    const cssPx = span + outsideMarginPx(first, last);
    return cssPx > 0 ? Math.round(cssPx) : 0;
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
    // A trim whose measured prefix is corrupt geometry. Holding stops the
    // layout effect from scheduling the same trim again on the revert render.
    const geometryHoldRef = useRef(false);

    // Before paint: a switched conversation must never inherit the previous
    // spacer height for a frame.
    useLayoutEffect(() => {
        pendingRef.current = null;
        geometryHoldRef.current = false;
        containerRef.current?.removeAttribute(TRANSCRIPT_WINDOW_ADJUSTING_ATTR);
        setStartId(null);
        setSpacerPx(0);
    }, [containerRef, conversationKey]);

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
        containerRef.current?.removeAttribute(TRANSCRIPT_WINDOW_ADJUSTING_ATTR);
        setStartId(null);
        setSpacerPx(0);
    }, [containerRef, messages, startId]);

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
            // Invariant: nothing unmounted means no spacer. A load correction
            // can under-subtract when the new rows have not taken their height
            // yet; left alone that remainder is a blank band at the top.
            setSpacerPx(0);
            return;
        }
        const scrollHeight = container.scrollHeight;
        const contentPx = Math.max(0, scrollHeight - spacerPxRef.current);
        const ceiling = conversationRenderTrimCeilingPx(viewportPx, list.length);

        if (!scrolledUpRef.current) {
            if (Date.now() < trimHoldUntilRef.current) return;
            if (geometryHoldRef.current) {
                if (contentPx <= ceiling) geometryHoldRef.current = false;
                else return;
            }
            const next = conversationRenderTrimStart({ messages: list, windowStart: currentStart, viewportPx, contentPx });
            if (next === null) return;
            const measured = measuredPrefixPx(body, list, currentStart, next);
            if (measured > ceiling) {
                geometryHoldRef.current = true;
                return;
            }
            if (measured > 0) {
                // Same commit as the unmount. A later scrollHeight delta would
                // see a collapsed document and anchor restore would follow it.
                container.removeAttribute(TRANSCRIPT_WINDOW_ADJUSTING_ATTR);
                setSpacerPx((prev) => prev + measured);
                setStartId(list[next].id);
                return;
            }
            container.setAttribute(TRANSCRIPT_WINDOW_ADJUSTING_ATTR, "");
            pendingRef.current = { kind: "trim", contentPx: scrollHeight, previousStartId: startIdRef.current };
            setStartId(list[next].id);
            return;
        }

        if (currentStart <= 0) return;
        // Rects are visual pixels under the app scale; the viewport and the
        // blank threshold are CSS pixels. Mixing them mounts the whole prefix
        // early, or skips a batch, whenever --ui-scale is not 1.
        const offset = (body.getBoundingClientRect().top - container.getBoundingClientRect().top) * cssPerVisualPx(container);
        // Blank means the viewport sits inside the spacer itself, so the spacer
        // must be positive: padding plus the archive banner can push the body
        // top past the threshold at scrollTop 0 without any blank area.
        if (offset > CONVERSATION_RENDER_BLANK_PX && spacerPxRef.current > 0) {
            // The reader dragged past everything that is mounted. Batches would
            // leave them staring at the spacer, so put the rest back at once.
            container.setAttribute(TRANSCRIPT_WINDOW_ADJUSTING_ATTR, "");
            pendingRef.current = { kind: "load", contentPx: scrollHeight };
            setSpacerPx(0);
            setStartId(null);
            return;
        }
        if (offset <= -viewportPx) return;
        const next = conversationRenderLoadStart({ messages: list, windowStart: currentStart, viewportPx, contentPx });
        if (next === null) return;
        container.setAttribute(TRANSCRIPT_WINDOW_ADJUSTING_ATTR, "");
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
                if (scrolledUpRef.current) {
                    // The reader left the tail while the trim was committing.
                    // Keep the rows they are scrolling into.
                    container.removeAttribute(TRANSCRIPT_WINDOW_ADJUSTING_ATTR);
                    setStartId(pending.previousStartId);
                    return;
                }
                const removed = pending.contentPx - contentPx;
                const ceiling = conversationRenderTrimCeilingPx(container.clientHeight, messagesRef.current.length);
                if (removed > ceiling) {
                    geometryHoldRef.current = true;
                    container.removeAttribute(TRANSCRIPT_WINDOW_ADJUSTING_ATTR);
                    setStartId(pending.previousStartId);
                    return;
                }
                if (removed > 0) setSpacerPx((prev) => prev + removed);
                // The spacer setter re-commits; pin there so the bottom does not
                // drift while the height is restored. The attribute stays until
                // that pin, so anchor restore does not see the collapsed frame.
                pendingRef.current = { kind: "trim-pin" };
                return;
            }
            if (pending.kind === "trim-pin") {
                container.removeAttribute(TRANSCRIPT_WINDOW_ADJUSTING_ATTR);
                if (!scrolledUpRef.current) container.scrollTop = container.scrollHeight;
                return;
            }
            container.removeAttribute(TRANSCRIPT_WINDOW_ADJUSTING_ATTR);
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
        containerRef.current?.removeAttribute(TRANSCRIPT_WINDOW_ADJUSTING_ATTR);
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
    }, [containerRef]);

    return { windowStart, spacerPx, spacerRef, revealMessage };
}
