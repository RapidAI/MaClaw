/**
 * Floating host for the 目标清单 card, pinned to the top-right of the chat
 * column (.aap-chat-column is position:relative). The transcript keeps the
 * goal card in history (kind: "goalChecklist") but no longer paints it inline,
 * so the step list stays visible while the conversation scrolls.
 *
 * Default is expanded. Collapsing leaves a compact bar that still shows the
 * goal summary; hovering the bar re-expands a preview (mouse-leave folds it
 * back), and clicking pins the card open until the header's collapse button
 * is pressed. The bar — and the expanded card's header — drags the float
 * anywhere inside the column.
 */
import { useEffect, useMemo, useRef, useState, type CSSProperties, type PointerEvent as ReactPointerEvent } from "react";
import { GoalChecklistCard, GoalTitleGlyph, goalChecklistBodyFromText, goalChecklistDoneCount, type GoalChecklistBody } from "./GoalChecklistCard";
import { localizeText } from "./aiAssistantI18n";
import type { Theme } from "./aiAssistantPanelTheme";

/** Pointer movement (px) before a press becomes a drag instead of a click. */
const GOAL_DRAG_THRESHOLD = 4;
/** Margin (px) kept between the dragged float and the parent edges. */
const GOAL_DRAG_MARGIN = 4;

function goalStatusChip(lang: string | undefined, body: GoalChecklistBody | null): { label: string; tone: "done" | "fail" | "hold" } | null {
    if (!body) return null;
    if (body.status === "complete") return { label: localizeText(lang, "Completed", "已完成", "已完成"), tone: "done" };
    if (body.status === "failed") return { label: localizeText(lang, "Failed", "已失败", "已失敗"), tone: "fail" };
    if (body.status === "paused") return { label: localizeText(lang, "Paused", "已暂停", "已暫停"), tone: "hold" };
    if (body.status === "stopped") return { label: localizeText(lang, "Stopped", "已停止", "已停止"), tone: "hold" };
    return null;
}

export function GoalChecklistOverlay({ lang, theme: t, text, defaultTop = 80 }: {
    lang?: string;
    theme: Theme;
    /** Published checklist body (目标清单\n…), the same text the transcript card parsed. */
    text: string;
    /** Resting top offset inside the chat column — below the task header (and the coding chip when present). */
    defaultTop?: number;
}) {
    const body = useMemo(() => goalChecklistBodyFromText(text), [text]);
    // Click-to-expand pins the card; hovering the collapsed bar only peeks.
    const [pinned, setPinned] = useState(true);
    const [hoverPeek, setHoverPeek] = useState(false);
    const expanded = pinned || hoverPeek;

    const rootRef = useRef<HTMLDivElement | null>(null);
    const [dragDelta, setDragDelta] = useState({ x: 0, y: 0 });
    const dragSessionRef = useRef<{
        pointerId: number;
        startX: number;
        startY: number;
        baseX: number;
        baseY: number;
        dragging: boolean;
    } | null>(null);
    // Swallow the click that follows a drag so it does not toggle the card.
    // Time-windowed (not a one-shot flag): if the pointer ends outside the
    // float, no click fires and a one-shot flag would eat the NEXT real click.
    const suppressClickUntilRef = useRef(0);
    const dragDeltaRef = useRef(dragDelta);
    dragDeltaRef.current = dragDelta;
    const pinnedRef = useRef(pinned);
    pinnedRef.current = pinned;
    // Removes the document-level drag listeners; nulled when the session ends.
    const dragCleanupRef = useRef<(() => void) | null>(null);
    useEffect(() => () => {
        dragCleanupRef.current?.();
        dragCleanupRef.current = null;
    }, []);

    // Clamp a candidate delta so the float stays inside the parent box.
    const clampDragDelta = (x: number, y: number): { x: number; y: number } => {
        const root = rootRef.current;
        const parent = root?.parentElement;
        if (!root || !parent) return { x, y };
        const pr = parent.getBoundingClientRect();
        // Unmeasured layout (e.g. jsdom): skip clamping rather than pinning to 0.
        if (pr.width <= 0 || pr.height <= 0) return { x, y };
        const rr = root.getBoundingClientRect();
        // Rest position: right edge 10px from parent right, top at defaultTop.
        const restLeft = pr.right - 10 - rr.width;
        const minX = pr.left + GOAL_DRAG_MARGIN - restLeft;
        const maxX = 0;
        const minY = GOAL_DRAG_MARGIN - defaultTop;
        const maxY = pr.height - GOAL_DRAG_MARGIN - defaultTop - rr.height;
        return {
            x: Math.min(maxX, Math.max(minX, x)),
            y: Math.min(maxY, Math.max(minY, y)),
        };
    };

    const beginDrag = (e: ReactPointerEvent) => {
        if (e.button !== 0) return;
        // A second pointer can arrive mid-drag (multi-touch): retire the old
        // session's document listeners so they cannot outlive their pointer.
        dragCleanupRef.current?.();
        dragSessionRef.current = {
            pointerId: e.pointerId,
            startX: e.clientX,
            startY: e.clientY,
            baseX: dragDeltaRef.current.x,
            baseY: dragDeltaRef.current.y,
            dragging: false,
        };
        const onMove = (ev: PointerEvent) => {
            const session = dragSessionRef.current;
            if (!session || ev.pointerId !== session.pointerId) return;
            const dx = ev.clientX - session.startX;
            const dy = ev.clientY - session.startY;
            if (!session.dragging && Math.hypot(dx, dy) < GOAL_DRAG_THRESHOLD) return;
            session.dragging = true;
            setDragDelta(clampDragDelta(session.baseX + dx, session.baseY + dy));
        };
        const onUp = (ev: PointerEvent) => {
            const session = dragSessionRef.current;
            if (!session || ev.pointerId !== session.pointerId) return;
            if (session.dragging) suppressClickUntilRef.current = Date.now() + 300;
            dragSessionRef.current = null;
            cleanup();
            // mouseleave was held back during the drag; if the pointer ended
            // outside the float, fold the hover preview now.
            if (!rootRef.current?.matches(":hover")) setHoverPeek(false);
        };
        const cleanup = () => {
            dragCleanupRef.current = null;
            document.removeEventListener("pointermove", onMove, true);
            document.removeEventListener("pointerup", onUp, true);
            document.removeEventListener("pointercancel", onUp, true);
        };
        document.addEventListener("pointermove", onMove, true);
        document.addEventListener("pointerup", onUp, true);
        document.addEventListener("pointercancel", onUp, true);
        dragCleanupRef.current = cleanup;
    };

    const swallowClickIfDragged = (): boolean => Date.now() < suppressClickUntilRef.current;

    const pinOpen = () => {
        if (swallowClickIfDragged()) return;
        setPinned(true);
    };

    const toggleHeadButton = () => {
        if (swallowClickIfDragged()) return;
        if (pinnedRef.current) {
            setPinned(false);
            setHoverPeek(false);
        } else {
            // Peeking: clicking the header button keeps the card open.
            setPinned(true);
        }
    };

    const doneCount = body ? goalChecklistDoneCount(body.steps) : 0;
    const statusChip = goalStatusChip(lang, body);
    const title = localizeText(lang, "Goal checklist", "目标清单", "目標清單");
    const summary = body && body.steps.length > 0
        ? `${doneCount}/${body.steps.length}`
        : (body?.activity || "").trim();
    const running = body ? body.status === "active" : false;

    const rootStyle: CSSProperties = {
        position: "absolute",
        top: defaultTop,
        right: 10,
        // No bottom pin: the card must be free to drag vertically. The column
        // box bounds it instead (percentages on an absolute element resolve
        // against the positioned ancestor) and the clamp keeps it inside.
        maxHeight: `calc(100% - ${defaultTop + 8}px)`,
        ...(dragDelta.x !== 0 || dragDelta.y !== 0
            ? { transform: `translate(${dragDelta.x}px, ${dragDelta.y}px)` }
            : null),
        zIndex: 30,
        display: "flex",
        flexDirection: "column",
        alignItems: "flex-end",
        maxWidth: "min(360px, calc(100% - 16px))",
        // Let chat scroll/select under the empty area of this root.
        pointerEvents: "none",
    };

    const barStyle: CSSProperties = {
        pointerEvents: "auto",
        display: "inline-flex",
        alignItems: "center",
        gap: 6,
        maxWidth: "min(340px, calc(100% - 8px))",
        minHeight: 32,
        padding: "0 10px 0 8px",
        borderRadius: 10,
        border: `1px solid ${t.fieldBorder}`,
        background: t.fieldBg,
        color: t.text,
        boxShadow: t.isDark
            ? "0 4px 14px rgba(0,0,0,0.35)"
            : "0 4px 14px rgba(15,23,42,0.10)",
        cursor: "pointer",
        // Let pointermove drive dragging on touch instead of scrolling.
        touchAction: "none",
        fontSize: 11,
        lineHeight: 1,
        fontFamily: "inherit",
        minWidth: 0,
        boxSizing: "border-box",
        overflow: "hidden",
        userSelect: "none",
    };

    const cardWrapStyle: CSSProperties = {
        pointerEvents: "auto",
        width: "min(340px, calc(100vw - 32px))",
        maxWidth: "100%",
        // Shrink inside the height-bounded flex root and scroll.
        minHeight: 0,
        flex: "0 1 auto",
        overflowY: "auto",
        overscrollBehavior: "contain",
        borderRadius: 12,
        boxShadow: t.isDark
            ? "0 16px 40px rgba(0,0,0,0.45), 0 0 0 1px rgba(148,163,184,0.12)"
            : "0 12px 30px rgba(27, 56, 93, 0.14)",
    };

    const headButton = (
        <button
            type="button"
            data-testid="goal-checklist-overlay-toggle"
            aria-label={pinned
                ? localizeText(lang, "Collapse goal checklist", "收起目标清单", "收起目標清單")
                : localizeText(lang, "Keep goal checklist open", "固定展开目标清单", "固定展開目標清單")}
            title={pinned
                ? localizeText(lang, "Collapse (hover the bar to preview again)", "收起（悬停清单条可预览）", "收起（懸停清單條可預覽）")
                : localizeText(lang, "Keep expanded", "保持展开", "保持展開")}
            onClick={toggleHeadButton}
            style={{
                display: "inline-flex",
                alignItems: "center",
                justifyContent: "center",
                width: 20,
                height: 20,
                padding: 0,
                border: "none",
                borderRadius: 6,
                background: "transparent",
                color: "inherit",
                cursor: "pointer",
                flexShrink: 0,
            }}
        >
            {pinned ? (
                <svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true">
                    <path d="M4 10l4-4 4 4" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
                </svg>
            ) : (
                <svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true">
                    <path d="M4 6l4 4 4-4" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
                </svg>
            )}
        </button>
    );

    return (
        <div
            ref={rootRef}
            data-testid="goal-checklist-float"
            data-expanded={expanded ? "true" : "false"}
            data-pinned={pinned ? "true" : "false"}
            style={rootStyle}
            onMouseEnter={() => { if (!pinnedRef.current) setHoverPeek(true); }}
            // Hold the preview open while a drag is in flight — the pointer
            // may ride past the moving card's edge; pointerup re-checks :hover.
            onMouseLeave={() => { if (!dragSessionRef.current) setHoverPeek(false); }}
        >
            {expanded ? (
                <div data-testid="goal-checklist-overlay-card" style={cardWrapStyle}>
                    {body ? (
                        <GoalChecklistCard lang={lang} body={body} headEnd={headButton} onHeadPointerDown={beginDrag} />
                    ) : (
                        // Unparsable stored text keeps a readable raw card instead of vanishing.
                        <section className="gkc-card" aria-label={title}>
                            <div className="gkc-head" onPointerDown={beginDrag} style={{ cursor: "grab", userSelect: "none", touchAction: "none" }}>
                                <span className="gkc-title">
                                    <span className="gkc-title-icon" aria-hidden="true"><GoalTitleGlyph /></span>
                                    {title}
                                </span>
                                <span className="gkc-head-end">{headButton}</span>
                            </div>
                            <div style={{ marginTop: 8, whiteSpace: "pre-wrap", overflowWrap: "break-word", fontSize: 12, lineHeight: 1.55 }}>
                                {text}
                            </div>
                        </section>
                    )}
                </div>
            ) : (
                <button
                    type="button"
                    data-testid="goal-checklist-overlay-bar"
                    aria-expanded="false"
                    title={localizeText(
                        lang,
                        "Goal checklist — hover to preview, click to keep open, drag to move",
                        "目标清单 — 悬停预览，点击保持展开，可拖动",
                        "目標清單 — 懸停預覽，點擊保持展開，可拖動",
                    )}
                    onClick={pinOpen}
                    onPointerDown={beginDrag}
                    style={barStyle}
                >
                    <span
                        aria-hidden="true"
                        style={{
                            display: "inline-flex",
                            alignItems: "center",
                            justifyContent: "center",
                            width: 18,
                            height: 18,
                            borderRadius: 5,
                            flexShrink: 0,
                            color: t.headingColor,
                            border: `1px solid ${t.fieldBorder}`,
                            boxSizing: "border-box",
                        }}
                    >
                        <GoalTitleGlyph />
                    </span>
                    <span style={{ fontWeight: 700, color: t.headingColor, whiteSpace: "nowrap" }}>{title}</span>
                    {running ? <span className="gkc-pulse-dot" aria-hidden="true" style={{ color: t.headingColor }} /> : null}
                    {summary ? (
                        <span style={{ color: t.textMuted, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis", maxWidth: 120 }}>
                            {summary}
                        </span>
                    ) : null}
                    {statusChip ? (
                        <span className={`gkc-chip gkc-chip--${statusChip.tone}`} style={{ padding: "1.5px 7px", fontSize: 10 }}>
                            {statusChip.label}
                        </span>
                    ) : null}
                    <span aria-hidden="true" style={{ color: t.textMuted, fontSize: 10, flexShrink: 0 }}>▾</span>
                </button>
            )}
        </div>
    );
}
