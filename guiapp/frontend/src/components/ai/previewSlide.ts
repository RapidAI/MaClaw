import { useEffect, useRef, useState } from "react";

/** Right-edge slide duration for preview panels, open and close. */
export const PREVIEW_SLIDE_MS = 240;

/** Honor the OS "reduce motion" setting: skip the slide entirely. */
export function usePreviewSlideDuration(): number {
    const [duration] = useState(() =>
        typeof window !== "undefined" && typeof window.matchMedia === "function"
        && window.matchMedia("(prefers-reduced-motion: reduce)").matches
            ? 0
            : PREVIEW_SLIDE_MS,
    );
    return duration;
}

export interface PreviewSlideState {
    /** Whether the overlay DOM should be mounted at all. */
    present: boolean;
    /** Whether the panel should render at its on-screen position. */
    entered: boolean;
    /** Whether an exit animation is in progress. */
    closing: boolean;
    /**
     * Whether the slide-in has fully completed and the transform was cleared.
     * A lingering non-none transform would make the panel the containing block
     * for fixed-position descendants (e.g. viewport-anchored context menus).
     */
    settled: boolean;
    /** Resolved animation duration (0 under prefers-reduced-motion). */
    slideMs: number;
}

/**
 * Shared right-edge slide lifecycle for preview overlays: slides in when
 * `open` turns true, slides back out when it turns false, and only then
 * invokes `onExited` (so callers can unmount / drop state after the
 * animation instead of the moment the close was requested).
 */
export function usePreviewSlideLifecycle(open: boolean, onExited?: () => void): PreviewSlideState {
    const slideMs = usePreviewSlideDuration();
    const [present, setPresent] = useState(open);
    const [entered, setEntered] = useState(false);
    const [closing, setClosing] = useState(false);
    const [settled, setSettled] = useState(false);
    const onExitedRef = useRef(onExited);
    // Ref writes belong in effects: assigning during render can tear under
    // concurrent rendering.
    useEffect(() => {
        onExitedRef.current = onExited;
    }, [onExited]);

    useEffect(() => {
        if (!open) return;
        setPresent(true);
        setClosing(false);
    }, [open]);

    useEffect(() => {
        if (!present || !open || entered) return;
        const id = window.setTimeout(() => setEntered(true), 20);
        return () => window.clearTimeout(id);
    }, [present, open, entered]);

    useEffect(() => {
        if (!entered || closing || settled) return;
        const id = window.setTimeout(() => setSettled(true), slideMs);
        return () => window.clearTimeout(id);
    }, [entered, closing, settled, slideMs]);

    useEffect(() => {
        if (!present || open) return;
        setClosing(true);
        setSettled(false);
        const id = window.setTimeout(() => {
            setPresent(false);
            setClosing(false);
            setEntered(false);
            onExitedRef.current?.();
        }, slideMs);
        return () => window.clearTimeout(id);
    }, [present, open, slideMs]);

    return { present, entered, closing, settled, slideMs };
}
