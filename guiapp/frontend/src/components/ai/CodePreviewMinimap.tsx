/**
 * Right-edge document minimap for CodePreviewPanel.
 *
 * Shows a thumbnail of the source (scaled text when it fits, density bars
 * otherwise) and a viewport overlay you can click or drag to jump by page.
 */
import React, { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import type { CodePreviewTheme } from './FileTabBar';
import type { DiffLine } from './diffCompute';
import {
    CODE_PREVIEW_MINIMAP_WIDTH,
    buildMinimapSamples,
    clampMinimapScrollTop,
    drawCodePreviewMinimap,
    minimapMarkerRatio,
    minimapPointerToScrollTop,
    minimapThumbFromScroll,
    minimapThumbsEqual,
    minimapWheelDeltaPixels,
    nextObservedScrollContent,
    type MinimapColors,
    type MinimapThumbMetrics,
} from './codePreviewMinimapHelpers';

const EMPTY_MATCHES: number[] = [];
const EMPTY_LINES: string[] = [];
const ZERO_THUMB: MinimapThumbMetrics = { top: 0, height: 0, maxScroll: 0, travel: 0 };

function trackHeightOf(root: HTMLElement): number {
    return root.clientHeight > 0 ? root.clientHeight : root.getBoundingClientRect().height;
}

/** Follow the scroll viewport's first child so wrap/zoom/markdown layout updates the thumb. */
function observeScrollContent(
    ro: ResizeObserver,
    scrollEl: HTMLElement,
    observedRef: React.MutableRefObject<Element | null>,
): void {
    const swap = nextObservedScrollContent(observedRef.current, scrollEl.firstElementChild);
    if (swap.unobserve) {
        try {
            ro.unobserve(swap.unobserve);
        } catch {
            // node may already be gone
        }
    }
    if (swap.observe) ro.observe(swap.observe);
    observedRef.current = swap.next;
}

function minimapColorsFromTheme(theme: CodePreviewTheme): MinimapColors {
    return {
        text: theme.textMuted,
        add: theme.diffAddText,
        delete: theme.diffDeleteText,
        match: '#eab308',
        activeMatch: '#facc15',
    };
}

export const CodePreviewMinimap = React.memo(function CodePreviewMinimap({
    scrollRef,
    scrollId,
    lines,
    diffLines = null,
    matchLineIndexes = EMPTY_MATCHES,
    activeMatchLine = -1,
    theme,
    lang = 'en',
}: {
    scrollRef: React.RefObject<HTMLDivElement | null>;
    scrollId?: string;
    lines: string[];
    diffLines?: DiffLine[] | null;
    matchLineIndexes?: readonly number[];
    activeMatchLine?: number;
    theme: CodePreviewTheme;
    lang?: string;
}) {
    const rootRef = useRef<HTMLDivElement>(null);
    const canvasRef = useRef<HTMLCanvasElement>(null);
    const ctxRef = useRef<CanvasRenderingContext2D | null>(null);
    const dragRef = useRef<{
        pointerId: number;
        grabOffset: number;
        trackHeight: number;
        top: number;
    } | null>(null);
    const thumbRafRef = useRef<number | null>(null);
    const paintRafRef = useRef<number | null>(null);
    const resizeObserverRef = useRef<ResizeObserver | null>(null);
    const observedContentRef = useRef<Element | null>(null);
    const [thumb, setThumb] = useState<MinimapThumbMetrics>(ZERO_THUMB);

    // Active-match is a cheap overlay so Find Next does not rebuild every row.
    const samples = useMemo(
        () => buildMinimapSamples({
            lines,
            diffLines,
            matchLineIndexes,
        }),
        [diffLines, lines, matchLineIndexes],
    );

    const sourceCount = diffLines && diffLines.length > 0 ? diffLines.length : lines.length;
    const thumbnailLines = useMemo(() => {
        if (samples.length !== sourceCount) return EMPTY_LINES;
        if (diffLines && diffLines.length > 0) return diffLines.map((dl) => dl.content);
        return lines;
    }, [diffLines, lines, samples.length, sourceCount]);

    const colors = useMemo(
        () => minimapColorsFromTheme(theme),
        [theme.textMuted, theme.diffAddText, theme.diffDeleteText],
    );
    const paintInputsRef = useRef({ samples, thumbnailLines, colors });
    paintInputsRef.current = { samples, thumbnailLines, colors };

    const markerRatio = useMemo(
        () => minimapMarkerRatio(activeMatchLine, lines.length, diffLines),
        [activeMatchLine, diffLines, lines.length],
    );

    const readThumb = useCallback((): MinimapThumbMetrics => {
        const el = scrollRef.current;
        const root = rootRef.current;
        if (!el || !root) return ZERO_THUMB;
        const trackHeight = trackHeightOf(root);
        return minimapThumbFromScroll(
            el.scrollTop,
            el.clientHeight,
            el.scrollHeight,
            trackHeight,
        );
    }, [scrollRef]);

    const syncThumb = useCallback(() => {
        if (thumbRafRef.current != null) return;
        thumbRafRef.current = window.requestAnimationFrame(() => {
            thumbRafRef.current = null;
            const next = readThumb();
            setThumb((prev) => (minimapThumbsEqual(prev, next) ? prev : next));
        });
    }, [readThumb]);

    const paint = useCallback(() => {
        const canvas = canvasRef.current;
        const root = rootRef.current;
        if (!canvas || !root) return;
        const cssW = root.clientWidth;
        const cssH = root.clientHeight;
        if (cssW <= 0 || cssH <= 0) return;
        const { samples: nextSamples, thumbnailLines: nextLines, colors: nextColors } = paintInputsRef.current;
        const dpr = window.devicePixelRatio || 1;
        const pixelW = Math.max(1, Math.round(cssW * dpr));
        const pixelH = Math.max(1, Math.round(cssH * dpr));
        const resized = canvas.width !== pixelW || canvas.height !== pixelH;
        if (resized) {
            canvas.width = pixelW;
            canvas.height = pixelH;
            ctxRef.current = null;
        }
        const cached = ctxRef.current;
        const ctx = cached ?? canvas.getContext('2d');
        if (!ctx) return;
        ctxRef.current = ctx;
        if (!cached || resized) {
            ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
            ctx.imageSmoothingEnabled = false;
        }
        drawCodePreviewMinimap(ctx, {
            width: cssW,
            height: cssH,
            samples: nextSamples,
            lines: nextLines,
            colors: nextColors,
        });
    }, []);

    const schedulePaint = useCallback(() => {
        if (paintRafRef.current != null) return;
        paintRafRef.current = window.requestAnimationFrame(() => {
            paintRafRef.current = null;
            paint();
        });
    }, [paint]);

    useLayoutEffect(() => {
        paint();
        // Content/tab switches change scrollHeight without a resize or scroll event.
        // Sync before paint so the thumb is not a frame behind.
        const next = readThumb();
        setThumb((prev) => (minimapThumbsEqual(prev, next) ? prev : next));
        const el = scrollRef.current;
        const ro = resizeObserverRef.current;
        if (el && ro) observeScrollContent(ro, el, observedContentRef);
    }, [paint, samples, thumbnailLines, colors, readThumb, scrollRef]);

    useEffect(() => {
        const el = scrollRef.current;
        const root = rootRef.current;
        if (!el) return;
        el.addEventListener('scroll', syncThumb, { passive: true });
        let ro: ResizeObserver | null = null;
        if (typeof ResizeObserver === 'function') {
            ro = new ResizeObserver((entries) => {
                syncThumb();
                if (root && entries.some((entry) => entry.target === root)) schedulePaint();
            });
            resizeObserverRef.current = ro;
            ro.observe(el);
            if (root) ro.observe(root);
            observeScrollContent(ro, el, observedContentRef);
        }
        return () => {
            el.removeEventListener('scroll', syncThumb);
            ro?.disconnect();
            resizeObserverRef.current = null;
            observedContentRef.current = null;
            if (thumbRafRef.current != null) {
                window.cancelAnimationFrame(thumbRafRef.current);
                thumbRafRef.current = null;
            }
            if (paintRafRef.current != null) {
                window.cancelAnimationFrame(paintRafRef.current);
                paintRafRef.current = null;
            }
        };
    }, [schedulePaint, scrollRef, syncThumb]);

    const applyPointer = useCallback((
        clientY: number,
        drag: { grabOffset: number; trackHeight: number; top: number },
    ) => {
        const el = scrollRef.current;
        if (!el) return;
        el.scrollTop = minimapPointerToScrollTop(
            clientY - drag.top,
            drag.trackHeight,
            el.clientHeight,
            el.scrollHeight,
            drag.grabOffset,
        );
    }, [scrollRef]);

    const onPointerDown = useCallback((e: React.PointerEvent<HTMLDivElement>) => {
        if (e.button !== 0) return;
        if (dragRef.current) return;
        const root = rootRef.current;
        if (!root) return;
        e.preventDefault();
        e.stopPropagation();
        if (typeof root.setPointerCapture === 'function') {
            root.setPointerCapture(e.pointerId);
        }
        try {
            root.focus({ preventScroll: true });
        } catch {
            root.focus();
        }
        const rect = root.getBoundingClientRect();
        const y = e.clientY - rect.top;
        const current = readThumb();
        const hitThumb = y >= current.top && y <= current.top + current.height;
        // Dragging the overlay keeps the grab point; clicking the track
        // centers the current page on the pointer (page locate).
        const grabOffset = hitThumb ? y - current.top : current.height / 2;
        const drag = {
            pointerId: e.pointerId,
            grabOffset,
            trackHeight: root.clientHeight > 0 ? root.clientHeight : rect.height,
            top: rect.top,
        };
        dragRef.current = drag;
        applyPointer(e.clientY, drag);
    }, [applyPointer, readThumb]);

    const stopDrag = useCallback((pointerId: number): boolean => {
        const drag = dragRef.current;
        if (!drag || drag.pointerId !== pointerId) return false;
        dragRef.current = null;
        return true;
    }, []);

    const onPointerUp = useCallback((e: React.PointerEvent<HTMLDivElement>) => {
        if (!stopDrag(e.pointerId)) return;
        try {
            if (typeof e.currentTarget.releasePointerCapture === 'function') {
                e.currentTarget.releasePointerCapture(e.pointerId);
            }
        } catch {
            // capture may already be released
        }
    }, [stopDrag]);

    const onPointerMove = useCallback((e: React.PointerEvent<HTMLDivElement>) => {
        const drag = dragRef.current;
        if (!drag || drag.pointerId !== e.pointerId) return;
        // Some WebViews omit pointerup after a capture loss; buttons=0 is the release.
        if (e.buttons === 0) {
            onPointerUp(e);
            return;
        }
        e.preventDefault();
        applyPointer(e.clientY, drag);
    }, [applyPointer, onPointerUp]);

    const onLostPointerCapture = useCallback((e: React.PointerEvent<HTMLDivElement>) => {
        stopDrag(e.pointerId);
    }, [stopDrag]);

    useEffect(() => {
        const root = rootRef.current;
        if (!root) return;
        const onWheel = (e: WheelEvent) => {
            const el = scrollRef.current;
            if (!el) return;
            el.scrollTop = clampMinimapScrollTop(
                el.scrollTop + minimapWheelDeltaPixels(e.deltaY, e.deltaMode, el.clientHeight),
                el.clientHeight,
                el.scrollHeight,
            );
            e.preventDefault();
        };
        root.addEventListener('wheel', onWheel, { passive: false });
        return () => root.removeEventListener('wheel', onWheel);
    }, [scrollRef]);

    const onKeyDown = useCallback((e: React.KeyboardEvent<HTMLDivElement>) => {
        const el = scrollRef.current;
        if (!el) return;
        const page = Math.max(1, el.clientHeight);
        let next: number | null = null;
        if (e.key === 'ArrowDown') next = el.scrollTop + 40;
        else if (e.key === 'ArrowUp') next = el.scrollTop - 40;
        else if (e.key === 'PageDown') next = el.scrollTop + page;
        else if (e.key === 'PageUp') next = el.scrollTop - page;
        else if (e.key === 'Home') next = 0;
        else if (e.key === 'End') next = el.scrollHeight;
        else if (e.key === ' ' && !e.ctrlKey && !e.metaKey && !e.altKey) {
            next = e.shiftKey ? el.scrollTop - page : el.scrollTop + page;
        }
        if (next == null) return;
        e.preventDefault();
        e.stopPropagation();
        el.scrollTop = clampMinimapScrollTop(next, el.clientHeight, el.scrollHeight);
    }, [scrollRef]);

    const isZh = lang.startsWith('zh');
    const isZhHant = lang === 'zh-Hant';
    const label = isZhHant ? '文件縮略圖，按頁定位' : isZh ? '文件缩略图，按页定位' : 'Document minimap, jump by page';
    const valueNow = thumb.travel <= 0
        ? 0
        : Math.round((thumb.top / thumb.travel) * thumb.maxScroll);
    const trackHeight = thumb.height + thumb.travel;
    const markerTop = markerRatio == null || trackHeight <= 0 ? null : markerRatio * trackHeight;

    return (
        <div
            ref={rootRef}
            className="mc-code-preview-minimap"
            data-testid="code-preview-minimap"
            role="scrollbar"
            aria-orientation="vertical"
            aria-label={label}
            aria-controls={scrollId}
            aria-valuemin={0}
            aria-valuemax={Math.round(thumb.maxScroll)}
            aria-valuenow={valueNow}
            tabIndex={thumb.maxScroll > 0 ? 0 : -1}
            title={label}
            onPointerDown={onPointerDown}
            onPointerMove={onPointerMove}
            onPointerUp={onPointerUp}
            onPointerCancel={onLostPointerCapture}
            onLostPointerCapture={onLostPointerCapture}
            onKeyDown={onKeyDown}
            style={{
                position: 'relative',
                flexShrink: 0,
                width: CODE_PREVIEW_MINIMAP_WIDTH,
                height: '100%',
                minHeight: 0,
                borderLeft: `1px solid ${theme.border}`,
                background: theme.lineNumBg,
                cursor: 'ns-resize',
                overflow: 'hidden',
                touchAction: 'none',
                userSelect: 'none',
            }}
        >
            <canvas
                ref={canvasRef}
                data-testid="code-preview-minimap-canvas"
                aria-hidden="true"
                style={{
                    position: 'absolute',
                    inset: 0,
                    width: '100%',
                    height: '100%',
                    pointerEvents: 'none',
                    display: 'block',
                }}
            />
            <div
                className="mc-code-preview-minimap-thumb"
                data-testid="code-preview-minimap-thumb"
                aria-hidden="true"
                style={{
                    position: 'absolute',
                    left: 1,
                    right: 1,
                    height: Math.max(thumb.height, 0),
                    transform: `translateY(${thumb.top}px)`,
                    pointerEvents: 'none',
                    borderRadius: 3,
                    color: theme.tabActiveText,
                    boxShadow: `inset 0 0 0 1px color-mix(in srgb, ${theme.tabActiveText} 28%, transparent)`,
                }}
            />
            {markerTop != null ? (
                <div
                    data-testid="code-preview-minimap-match-marker"
                    aria-hidden="true"
                    style={{
                        position: 'absolute',
                        left: 0,
                        right: 0,
                        height: 2,
                        transform: `translateY(${markerTop - 1}px)`,
                        pointerEvents: 'none',
                        background: colors.activeMatch,
                    }}
                />
            ) : null}
        </div>
    );
});
