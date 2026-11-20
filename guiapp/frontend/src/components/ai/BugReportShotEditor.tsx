import { useEffect, useLayoutEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from "react";
import { localizeText } from "./aiAssistantI18n";
import {
    BUG_REPORT_MARK_COLORS,
    bugReportAnnotatedDataURL,
    bugReportCanvasBox,
    bugReportFrameWidth,
    bugReportImageReady,
    extendBugReportMark,
    bugReportMarkColor,
    bugReportMarkWorthKeeping,
    bugReportPointerPoint,
    bugReportPointerSamples,
    emptyBugReportShotExport,
    paintBugReportMarks,
    type BugReportMark,
    type BugReportMarkTool,
    type BugReportShotExporter,
} from "./bugReportAnnotate";

const TOOLS: BugReportMarkTool[] = ["highlight", "pen", "box"];
const BUG_REPORT_UNDO_LIMIT = 50;

export function BugReportShotEditor({
    lang,
    src,
    disabled,
    exporter,
    onPreviewError,
    active = true,
}: {
    lang: string;
    src: string;
    disabled: boolean;
    exporter: { current: BugReportShotExporter };
    onPreviewError: () => void;
    // The About dialog keeps one editor per picture and hides the rest.
    // A hidden editor must not take Ctrl+Z, and it measures again when shown.
    active?: boolean;
}) {
    const imgRef = useRef<HTMLImageElement>(null);
    const canvasRef = useRef<HTMLCanvasElement>(null);
    const marksRef = useRef<BugReportMark[]>([]);
    const draftRef = useRef<BugReportMark | null>(null);
    const [marks, setMarks] = useState<BugReportMark[]>([]);
    const [ready, setReady] = useState(false);
    const pointerRef = useRef<number | null>(null);
    const captureElRef = useRef<HTMLCanvasElement | null>(null);
    const paintRef = useRef<() => void>(() => {});
    const rafRef = useRef(0);
    const viewRef = useRef({ width: 0, height: 0 });
    const undoRef = useRef<() => void>(() => {});
    const commitRef = useRef<() => void>(() => {});
    // Snapshots of the mark list before each commit and each clear. Undo
    // walks these, so 清除标注 is one step and does not throw the strokes away.
    const historyRef = useRef<BugReportMark[][]>([]);
    const [historyCount, setHistoryCount] = useState(0);
    const [tool, setTool] = useState<BugReportMarkTool>("highlight");
    const [color, setColor] = useState<string>(BUG_REPORT_MARK_COLORS[0].value);

    useEffect(() => {
        exporter.current = () => {
            const draftMark = draftRef.current;
            const current = draftMark && bugReportMarkWorthKeeping(draftMark)
                ? [...marksRef.current, draftMark]
                : marksRef.current;
            if (current.length === 0) return emptyBugReportShotExport();
            const img = imgRef.current;
            if (!img || img.naturalWidth <= 0 || img.naturalHeight <= 0) {
                return { annotated: "", failed: true };
            }
            try {
                const annotated = bugReportAnnotatedDataURL(img, img.naturalWidth, img.naturalHeight, current);
                if (!annotated) return { annotated: "", failed: true };
                return { annotated, failed: false };
            } catch {
                return { annotated: "", failed: true };
            }
        };
        return () => {
            exporter.current = emptyBugReportShotExport;
        };
    }, [exporter]);

    const schedulePaint = () => {
        if (rafRef.current !== 0) return;
        const run = () => {
            rafRef.current = 0;
            paintRef.current();
        };
        if (typeof requestAnimationFrame !== "function") {
            run();
            return;
        }
        rafRef.current = requestAnimationFrame(run);
    };

    useLayoutEffect(() => {
        const img = imgRef.current;
        if (bugReportImageReady(img)) setReady(true);
    }, [src]);

    useLayoutEffect(() => {
        if (!active) return;
        const canvas = canvasRef.current;
        const img = imgRef.current;
        if (!canvas || !img || !ready) return;
        let placing = false;
        const paint = () => {
            const view = viewRef.current;
            if (view.width <= 0 || view.height <= 0) return;
            const dpr = window.devicePixelRatio || 1;
            const bitmapW = Math.max(1, Math.round(view.width * dpr));
            const bitmapH = Math.max(1, Math.round(view.height * dpr));
            if (canvas.width !== bitmapW || canvas.height !== bitmapH) {
                canvas.width = bitmapW;
                canvas.height = bitmapH;
            }
            const ctx = canvas.getContext("2d");
            if (!ctx) return;
            ctx.setTransform(1, 0, 0, 1, 0, 0);
            ctx.clearRect(0, 0, canvas.width, canvas.height);
            // The bitmap size is rounded. Scale by that size so the picture's
            // last pixel lands inside the buffer instead of half a pixel past it.
            ctx.setTransform(bitmapW / view.width, 0, 0, bitmapH / view.height, 0, 0);
            paintBugReportMarks(ctx, view.width, view.height, marksRef.current);
            const draftMark = draftRef.current;
            if (draftMark) paintBugReportMarks(ctx, view.width, view.height, [draftMark]);
        };
        const place = () => {
            if (placing) return;
            placing = true;
            try {
                const host = canvas.parentElement;
                if (!host) return;
                // Drop the inline width first so a later, taller viewport can
                // grow the picture. Then hug the used width. Measuring the
                // fit-content box would keep the intrinsic width.
                host.style.width = "";
                const measured = img.getBoundingClientRect();
                const hugged = bugReportFrameWidth(measured.width);
                if (hugged) host.style.width = hugged;
                const imageRect = hugged ? img.getBoundingClientRect() : measured;
                const hostStyle = window.getComputedStyle(host);
                const box = bugReportCanvasBox(
                    imageRect,
                    host.getBoundingClientRect(),
                    parseFloat(hostStyle.borderLeftWidth) || 0,
                    parseFloat(hostStyle.borderTopWidth) || 0,
                );
                if (!box) return;
                canvas.style.left = `${box.left}px`;
                canvas.style.top = `${box.top}px`;
                canvas.style.width = `${box.width}px`;
                canvas.style.height = `${box.height}px`;
                const laid = canvas.getBoundingClientRect();
                if (laid.width <= 0 || laid.height <= 0) return;
                viewRef.current = { width: laid.width, height: laid.height };
                paint();
            } finally {
                placing = false;
            }
        };
        paintRef.current = paint;
        place();
        const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(() => place());
        observer?.observe(img);
        // A DPI move can change the bitmap scale without changing the image's
        // CSS box, so the image observer never runs. Resize still does.
        window.addEventListener("resize", place);
        return () => {
            observer?.disconnect();
            window.removeEventListener("resize", place);
            if (rafRef.current !== 0 && typeof cancelAnimationFrame === "function") {
                cancelAnimationFrame(rafRef.current);
            }
            rafRef.current = 0;
            viewRef.current = { width: 0, height: 0 };
            paintRef.current = () => {};
        };
    }, [ready, active]);

    const releaseCapturedPointer = () => {
        const canvas = captureElRef.current;
        const pointerId = pointerRef.current;
        captureElRef.current = null;
        pointerRef.current = null;
        if (!canvas || pointerId === null) return;
        try {
            if (canvas.hasPointerCapture?.(pointerId)) canvas.releasePointerCapture(pointerId);
        } catch {
            // The pointer is already gone.
        }
    };

    const pushHistory = () => {
        const stack = historyRef.current;
        stack.push(marksRef.current.slice());
        if (stack.length > BUG_REPORT_UNDO_LIMIT) stack.shift();
        setHistoryCount(stack.length);
    };

    const undo = () => {
        if (draftRef.current || pointerRef.current !== null) {
            draftRef.current = null;
            releaseCapturedPointer();
            schedulePaint();
            return;
        }
        const prev = historyRef.current.pop();
        setHistoryCount(historyRef.current.length);
        if (!prev) return;
        marksRef.current = prev;
        setMarks(prev);
        schedulePaint();
    };
    undoRef.current = undo;

    const clearMarks = () => {
        const hadMarks = marksRef.current.length > 0;
        draftRef.current = null;
        releaseCapturedPointer();
        if (!hadMarks) {
            schedulePaint();
            return;
        }
        pushHistory();
        marksRef.current = [];
        setMarks([]);
        schedulePaint();
    };

    useEffect(() => {
        if (!active) return;
        const onKey = (event: KeyboardEvent) => {
            if (disabled) return;
            if (!(event.ctrlKey || event.metaKey) || event.key.toLowerCase() !== "z" || event.shiftKey) return;
            const target = event.target as HTMLElement | null;
            if (target && (target.tagName === "TEXTAREA" || target.tagName === "INPUT" || target.isContentEditable)) return;
            if (!draftRef.current && historyRef.current.length === 0) return;
            event.preventDefault();
            undoRef.current();
        };
        const onBlur = () => {
            if (pointerRef.current === null && !draftRef.current) return;
            commitRef.current();
        };
        window.addEventListener("keydown", onKey);
        window.addEventListener("blur", onBlur);
        return () => {
            window.removeEventListener("keydown", onKey);
            window.removeEventListener("blur", onBlur);
        };
    }, [disabled, active]);

    const pointOf = (event: ReactPointerEvent<HTMLCanvasElement>) => {
        return bugReportPointerPoint(event.currentTarget.getBoundingClientRect(), event.clientX, event.clientY);
    };

    const commitDraft = () => {
        const finished = draftRef.current;
        draftRef.current = null;
        releaseCapturedPointer();
        // Re-entry from releasePointerCapture sees an empty draft. It must
        // not paint before the stroke is stored, and it must not schedule a
        // frame that would clear the stroke.
        if (!finished) return;
        if (!bugReportMarkWorthKeeping(finished)) {
            schedulePaint();
            return;
        }
        pushHistory();
        const next = [...marksRef.current, finished];
        marksRef.current = next;
        setMarks(next);
        schedulePaint();
    };
    commitRef.current = commitDraft;

    // The About dialog hides this editor without unmounting it. Finish the
    // stroke here: a pointer that is still down would otherwise block the next one.
    useEffect(() => {
        if (active) return;
        commitRef.current();
    }, [active]);

    // includeRelease is for pointerup. A move's coalesced list already ends
    // on that event; an up event's list often does not, and a short stroke
    // may never have fired a move at all.
    const extendFromPointer = (event: ReactPointerEvent<HTMLCanvasElement>, includeRelease: boolean) => {
        const draftMark = draftRef.current;
        if (!draftMark || pointerRef.current !== event.pointerId) return false;
        const rect = event.currentTarget.getBoundingClientRect();
        const samples = bugReportPointerSamples(event.nativeEvent);
        if (includeRelease) samples.push({ clientX: event.clientX, clientY: event.clientY });
        let grew = false;
        for (const sample of samples) {
            const point = bugReportPointerPoint(rect, sample.clientX, sample.clientY);
            if (!point) continue;
            if (extendBugReportMark(draftMark, point)) grew = true;
        }
        return grew;
    };

    const finishStroke = (event: ReactPointerEvent<HTMLCanvasElement>) => {
        // A second finger, or the capture-release that re-enters this handler,
        // must not drop the stroke the first call already took.
        if (pointerRef.current !== null && event.pointerId !== pointerRef.current) return;
        if (pointerRef.current === event.pointerId && extendFromPointer(event, true)) schedulePaint();
        commitDraft();
    };

    const onPointerDown = (event: ReactPointerEvent<HTMLCanvasElement>) => {
        if (disabled || !ready || viewRef.current.width <= 0 || event.button !== 0 || pointerRef.current !== null) return;
        const point = pointOf(event);
        if (!point) return;
        event.preventDefault();
        // preventDefault cancels the browser's own focus. The dialog opens
        // with the description focused, so the canvas has to take focus or
        // Ctrl+Z keeps editing that field.
        event.currentTarget.focus({ preventScroll: true });
        pointerRef.current = event.pointerId;
        captureElRef.current = event.currentTarget;
        try {
            event.currentTarget.setPointerCapture(event.pointerId);
        } catch {
            // Moves still arrive while the pointer stays over the canvas.
        }
        draftRef.current = { tool, color: bugReportMarkColor(color), points: [point] };
        schedulePaint();
    };

    const onPointerMove = (event: ReactPointerEvent<HTMLCanvasElement>) => {
        if (!draftRef.current || pointerRef.current !== event.pointerId) return;
        // A step smaller than the keep threshold still belongs to this
        // stroke. Leaving it to the dialog lets a tiny move start a scroll.
        event.preventDefault();
        if (extendFromPointer(event, false)) schedulePaint();
    };

    const onPointerLeave = (event: ReactPointerEvent<HTMLCanvasElement>) => {
        if (pointerRef.current !== event.pointerId) return;
        if (event.currentTarget.hasPointerCapture?.(event.pointerId)) return;
        finishStroke(event);
    };

    const toolLabel = (id: BugReportMarkTool) => {
        if (id === "highlight") return localizeText(lang, "Highlighter", "荧光笔", "螢光筆");
        if (id === "pen") return localizeText(lang, "Pen", "自由画笔", "自由畫筆");
        return localizeText(lang, "Box", "画框", "畫框");
    };

    const colorLabel = (id: string) => {
        if (id === "yellow") return localizeText(lang, "Yellow", "黄色", "黃色");
        if (id === "red") return localizeText(lang, "Red", "红色", "紅色");
        if (id === "blue") return localizeText(lang, "Blue", "蓝色", "藍色");
        if (id === "green") return localizeText(lang, "Green", "绿色", "綠色");
        return localizeText(lang, "Black", "黑色", "黑色");
    };

    const canUndo = historyCount > 0 && !disabled;
    const canClear = marks.length > 0 && !disabled;

    return (
        <>
            <p className="gui-bug-report-annotate-label">{localizeText(lang, "Mark what should stand out", "在截图上标出需要强调的位置", "在截圖上標出需要強調的位置")}</p>
            <div className="gui-bug-report-shot-wrap">
                <div className="gui-bug-report-shot" data-tool={tool} data-disabled={disabled ? "true" : "false"}>
                    <img
                        ref={imgRef}
                        src={src}
                        alt={localizeText(lang, "Current window screenshot", "当前界面截图", "目前介面截圖")}
                        draggable={false}
                        decoding="async"
                        onLoad={() => setReady(true)}
                        onError={onPreviewError}
                    />
                    <canvas
                        ref={canvasRef}
                        tabIndex={-1}
                        aria-label={localizeText(lang, "Draw on the screenshot", "在截图上标注", "在截圖上標註")}
                        onPointerDown={onPointerDown}
                        onPointerMove={onPointerMove}
                        onPointerUp={finishStroke}
                        onPointerCancel={finishStroke}
                        onLostPointerCapture={finishStroke}
                        onPointerLeave={onPointerLeave}
                    />
                </div>
            </div>
            <div className="gui-bug-report-tools" role="toolbar" aria-label={localizeText(lang, "Screenshot marks", "截图标注", "截圖標註")}>
                <div className="gui-bug-report-tools__group">
                    {TOOLS.map((id) => (
                        <button
                            key={id}
                            className="gui-bug-report-tool"
                            type="button"
                            aria-pressed={tool === id}
                            disabled={disabled}
                            onClick={() => setTool(id)}
                        >
                            <ToolGlyph tool={id} />
                            {toolLabel(id)}
                        </button>
                    ))}
                </div>
                <div className="gui-bug-report-tools__group" role="group" aria-label={localizeText(lang, "Color", "颜色", "顏色")}>
                    {BUG_REPORT_MARK_COLORS.map((item) => (
                        <button
                            key={item.id}
                            className="gui-bug-report-swatch"
                            type="button"
                            style={{ backgroundColor: item.value }}
                            aria-label={colorLabel(item.id)}
                            aria-pressed={color === item.value}
                            disabled={disabled}
                            onClick={() => setColor(item.value)}
                        />
                    ))}
                </div>
                <span className="gui-bug-report-tools__spacer" />
                <button className="gui-bug-report-tool" type="button" disabled={!canUndo} onClick={undo}>
                    {localizeText(lang, "Undo", "撤销", "撤銷")}
                </button>
                <button className="gui-bug-report-tool" type="button" disabled={!canClear} onClick={clearMarks}>
                    {localizeText(lang, "Clear marks", "清除标注", "清除標註")}
                </button>
            </div>
        </>
    );
}

function ToolGlyph({ tool }: { tool: BugReportMarkTool }) {
    if (tool === "highlight") {
        return (
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <path d="m9 11-6 6v3h3l6-6" />
                <path d="m22 12-4.6 4.6a2 2 0 0 1-2.8 0l-5.2-5.2a2 2 0 0 1 0-2.8L14 4" />
            </svg>
        );
    }
    if (tool === "pen") {
        return (
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <path d="M12 20h9" />
                <path d="M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4Z" />
            </svg>
        );
    }
    return (
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
            <rect x="4" y="6" width="16" height="12" rx="1.5" />
        </svg>
    );
}
