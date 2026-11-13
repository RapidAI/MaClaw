// Shared freehand drawing overlay. The SVG stretches over its relatively
// positioned host; stored strokes are normalized fractions of that box, so a
// PDF page, a scrollable document body, or a slide image all paint the same
// list. With the pen off the layer has no pointer events, so text selection
// and the right-click menu keep working underneath.

import { useEffect, useRef, useState } from "react";
import {
    newPdfDrawingId,
    PDF_DRAWING_MAX_POINTS,
    pdfDrawingHitsRect,
    type PdfDrawing,
    type PdfDrawingPoint,
    type PdfDrawingTool,
} from "./pdfDrawings";
import { pdfMarkFill, type PdfMarkColor, type PdfMarkRect } from "./pdfMarks";

// A stronger ink than the highlighter fill so a thin stroke stays readable on
// white paper while the palette still matches the highlight colours.
export const COMPANION_DRAWING_INK: Record<PdfMarkColor, string> = {
    yellow: "#c9930a",
    green: "#2f8f5b",
    blue: "#2f6bbd",
    pink: "#c74884",
};

// Eraser is a reader mode, not a stored drawing tool.
export type CompanionDrawTool = PdfDrawingTool | "eraser";

export function CompanionDrawingLayer({
    page,
    drawings,
    active,
    color,
    tool,
    onAdd,
    onErase,
    testId,
}: {
    /** Page stamp stored on a new drawing: the PDF page, the slide number, or
     *  1 for a one-page document body. */
    page: number;
    drawings: PdfDrawing[];
    active: boolean;
    color: PdfMarkColor;
    tool: CompanionDrawTool;
    onAdd: (drawing: PdfDrawing) => void;
    onErase: (ids: string[]) => void;
    testId?: string;
}) {
    const surfaceRef = useRef<SVGSVGElement>(null);
    const [draft, setDraft] = useState<PdfDrawing | null>(null);
    const draftRef = useRef<PdfDrawing | null>(null);
    const [eraseRect, setEraseRect] = useState<PdfMarkRect | null>(null);
    const eraseStartRef = useRef<PdfDrawingPoint | null>(null);
    const eraseRectRef = useRef<PdfMarkRect | null>(null);
    const drawingsRef = useRef(drawings);
    drawingsRef.current = drawings;
    const stopRef = useRef<(() => void) | null>(null);
    const cleanupRef = useRef<(() => void) | null>(null);

    // The pointer handlers live on window for the duration of one stroke. An
    // unmount leaving them behind would keep acting on a canvas that is gone.
    useEffect(() => () => cleanupRef.current?.(), []);
    useEffect(() => {
        if (active) return;
        // The pen can be switched off after a mouseup that landed outside the
        // window. Throw that unfinished stroke away instead of painting it on
        // the next click.
        cleanupRef.current?.();
        cleanupRef.current = null;
        stopRef.current = null;
        draftRef.current = null;
        eraseStartRef.current = null;
        eraseRectRef.current = null;
        setDraft(null);
        setEraseRect(null);
    }, [active]);

    // Switching tools abandons a gesture whose mouseup was lost outside the
    // window; otherwise the next click would commit that stale selection.
    useEffect(() => {
        cleanupRef.current?.();
        cleanupRef.current = null;
        stopRef.current = null;
        draftRef.current = null;
        eraseStartRef.current = null;
        eraseRectRef.current = null;
        setDraft(null);
        setEraseRect(null);
    }, [tool, page]);

    const pointAt = (clientX: number, clientY: number): PdfDrawingPoint | null => {
        const box = surfaceRef.current?.getBoundingClientRect();
        if (!box || box.width < 1 || box.height < 1) return null;
        return {
            x: Math.min(1, Math.max(0, (clientX - box.left) / box.width)),
            y: Math.min(1, Math.max(0, (clientY - box.top) / box.height)),
        };
    };

    // The eraser is a marquee: drag a box and every local stroke or frame it
    // touches goes away. Host drawings received from a share carry `shared`
    // and stay, matching the undo guard.
    const beginErase = (start: PdfDrawingPoint) => {
        eraseStartRef.current = start;
        const initial = { x: start.x, y: start.y, w: 0, h: 0 };
        eraseRectRef.current = initial;
        setEraseRect(initial);
        let frame = 0;
        let finish: () => void = () => undefined;
        const paintRect = () => {
            frame = 0;
            setEraseRect(eraseRectRef.current);
        };
        const schedulePaint = () => {
            if (frame) return;
            if (typeof window.requestAnimationFrame !== "function") {
                paintRect();
                return;
            }
            frame = window.requestAnimationFrame(paintRect);
        };
        const cancelPaint = () => {
            if (!frame) return;
            if (typeof window.cancelAnimationFrame === "function") window.cancelAnimationFrame(frame);
            frame = 0;
        };
        const move = (moveEvent: MouseEvent) => {
            const from = eraseStartRef.current;
            if (!from) return;
            const next = pointAt(moveEvent.clientX, moveEvent.clientY);
            if (!next) return;
            eraseRectRef.current = {
                x: Math.min(from.x, next.x),
                y: Math.min(from.y, next.y),
                w: Math.abs(next.x - from.x),
                h: Math.abs(next.y - from.y),
            };
            schedulePaint();
        };
        const removeListeners = () => {
            cancelPaint();
            window.removeEventListener("mousemove", move, true);
            window.removeEventListener("mouseup", finish, true);
            window.removeEventListener("blur", finish);
            if (cleanupRef.current === removeListeners) cleanupRef.current = null;
        };
        finish = () => {
            removeListeners();
            stopRef.current = null;
            const rect = eraseRectRef.current;
            eraseStartRef.current = null;
            eraseRectRef.current = null;
            setEraseRect(null);
            // A click without a drag must not erase a whole stroke.
            if (!rect || Math.max(rect.w, rect.h) < 0.002) return;
            const ids = drawingsRef.current
                .filter((drawing) => !drawing.shared && pdfDrawingHitsRect(drawing, rect))
                .map((drawing) => drawing.id);
            if (ids.length > 0) onErase(ids);
        };
        cleanupRef.current = removeListeners;
        stopRef.current = finish;
        window.addEventListener("mousemove", move, true);
        window.addEventListener("mouseup", finish, true);
        window.addEventListener("blur", finish);
    };

    const startDrawing = (event: React.MouseEvent) => {
        if (!active || event.button !== 0) return;
        event.preventDefault();
        const point = pointAt(event.clientX, event.clientY);
        if (!point) return;
        stopRef.current?.();
        if (tool === "eraser") {
            beginErase(point);
            return;
        }
        const shapeTool: PdfDrawingTool = tool === "box" ? "box" : "line";
        const drawing: PdfDrawing = {
            id: newPdfDrawingId(),
            page,
            color,
            tool: shapeTool,
            points: [point, point],
        };
        draftRef.current = drawing;
        setDraft(drawing);
        let frame = 0;
        let finish: () => void = () => undefined;
        const paintDraft = () => {
            frame = 0;
            setDraft(draftRef.current);
        };
        // Mouse move can fire faster than the display refresh. Repaint once per
        // frame instead of re-rendering the whole stroke on every event.
        const schedulePaint = () => {
            if (frame) return;
            if (typeof window.requestAnimationFrame !== "function") {
                paintDraft();
                return;
            }
            frame = window.requestAnimationFrame(paintDraft);
        };
        const cancelPaint = () => {
            if (!frame) return;
            if (typeof window.cancelAnimationFrame === "function") window.cancelAnimationFrame(frame);
            frame = 0;
        };
        const move = (moveEvent: MouseEvent) => {
            const current = draftRef.current;
            const box = surfaceRef.current?.getBoundingClientRect();
            if (!current || !box || box.width < 1 || box.height < 1) return;
            const next = pointAt(moveEvent.clientX, moveEvent.clientY);
            if (!next) return;
            if (current.tool === "line") {
                const last = current.points[current.points.length - 1];
                if (current.points.length >= PDF_DRAWING_MAX_POINTS) return;
                if (Math.abs(next.x - last.x) * box.width + Math.abs(next.y - last.y) * box.height < 1.5) return;
                draftRef.current = { ...current, points: [...current.points, next] };
                schedulePaint();
                return;
            }
            draftRef.current = { ...current, points: [current.points[0], next] };
            schedulePaint();
        };
        const removeListeners = () => {
            cancelPaint();
            window.removeEventListener("mousemove", move, true);
            window.removeEventListener("mouseup", finish, true);
            window.removeEventListener("blur", finish);
            if (cleanupRef.current === removeListeners) cleanupRef.current = null;
        };
        finish = () => {
            removeListeners();
            stopRef.current = null;
            const finished = draftRef.current;
            draftRef.current = null;
            setDraft(null);
            if (!finished) return;
            if (finished.tool === "line") {
                if (finished.points.length < 2) return;
            } else {
                const a = finished.points[0];
                const b = finished.points[finished.points.length - 1];
                if (Math.abs(b.x - a.x) < 0.002 && Math.abs(b.y - a.y) < 0.002) return;
            }
            onAdd(finished);
        };
        cleanupRef.current = removeListeners;
        stopRef.current = finish;
        window.addEventListener("mousemove", move, true);
        window.addEventListener("mouseup", finish, true);
        window.addEventListener("blur", finish);
    };

    const shapes = draft ? [...drawings, draft] : drawings;
    const erasing = active && tool === "eraser";
    const cursorClass = erasing
        ? " companion-pdf-drawings--eraser"
        : active && tool === "line"
            ? " companion-pdf-drawings--pen"
            : "";
    const doomed = tool === "eraser" && eraseRect
        ? new Set(
            drawings
                .filter((drawing) => !drawing.shared && pdfDrawingHitsRect(drawing, eraseRect))
                .map((drawing) => drawing.id),
        )
        : null;
    return (
        <svg
            ref={surfaceRef}
            className={active
                ? `companion-pdf-drawings companion-pdf-drawings--active${cursorClass}`
                : "companion-pdf-drawings"}
            data-testid={testId ?? `pdf-drawing-layer-${page}`}
            viewBox="0 0 1 1"
            preserveAspectRatio="none"
            aria-hidden="true"
            onMouseDown={startDrawing}
        >
            {shapes.map((drawing) => {
                const marked = doomed?.has(drawing.id) === true;
                const stroke = marked ? "#dc1f25" : COMPANION_DRAWING_INK[drawing.color];
                const opacity = marked ? 0.35 : undefined;
                if (drawing.tool === "box") {
                    const a = drawing.points[0];
                    const b = drawing.points[drawing.points.length - 1];
                    return (
                        <rect
                            key={drawing.id}
                            data-drawing-id={drawing.id}
                            x={Math.min(a.x, b.x)}
                            y={Math.min(a.y, b.y)}
                            width={Math.abs(b.x - a.x)}
                            height={Math.abs(b.y - a.y)}
                            fill={pdfMarkFill(drawing.color)}
                            fillOpacity={0.16}
                            stroke={stroke}
                            strokeWidth={2}
                            opacity={opacity}
                            vectorEffect="non-scaling-stroke"
                        />
                    );
                }
                return (
                    <polyline
                        key={drawing.id}
                        data-drawing-id={drawing.id}
                        points={drawing.points.map((point) => `${point.x},${point.y}`).join(" ")}
                        fill="none"
                        stroke={stroke}
                        strokeWidth={2.4}
                        strokeLinecap="round"
                        strokeLinejoin="round"
                        opacity={opacity}
                        vectorEffect="non-scaling-stroke"
                    />
                );
            })}
            {tool === "eraser" && eraseRect ? (
                <rect
                    data-testid={`pdf-erase-preview-${page}`}
                    x={eraseRect.x}
                    y={eraseRect.y}
                    width={eraseRect.w}
                    height={eraseRect.h}
                    fill="#dc1f25"
                    fillOpacity={0.08}
                    stroke="#dc1f25"
                    strokeWidth={1.5}
                    strokeDasharray="6 4"
                    pointerEvents="none"
                    vectorEffect="non-scaling-stroke"
                />
            ) : null}
        </svg>
    );
}
