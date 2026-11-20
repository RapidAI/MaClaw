import { useLayoutEffect, useRef, useState, type CSSProperties, type RefObject } from "react";
import { tailText } from "../../i18n/mnTail";

/** Shared by the measure and the visible label, so the fit check uses the drawn font. */
export const COMPOSER_SHORTCUT_HINT_FONT: CSSProperties = {
    fontSize: "12px",
    lineHeight: 1.2,
    whiteSpace: "nowrap",
};

/**
 * The measure lives in a zero-size clip. Its own box is max-content, so a
 * narrow send cluster cannot clamp the reading, and the clip cannot stretch
 * that cluster.
 */
export const COMPOSER_SHORTCUT_MEASURE_HOST_STYLE: CSSProperties = {
    position: "absolute",
    left: 0,
    top: 0,
    width: 0,
    height: 0,
    overflow: "hidden",
    visibility: "hidden",
    pointerEvents: "none",
};

export const COMPOSER_SHORTCUT_MEASURE_STYLE: CSSProperties = {
    ...COMPOSER_SHORTCUT_HINT_FONT,
    display: "inline-block",
    width: "max-content",
    pointerEvents: "none",
};

/** One sentence. The inline label and the send tooltip both read it. */
export function composerShortcutHint(lang: string | undefined): string {
    return tailText(
        lang ?? "",
        "Enter to send · Ctrl/⌘+Enter for a new line",
        "Enter 发送 · Ctrl/⌘+Enter 换行",
    );
}

export interface ComposerShortcutHintRoom {
    /** Toolbar content box, after horizontal padding. */
    toolbarWidth: number;
    /** Model slot. 0 when that slot is absent. */
    endWidth: number;
    toolbarGap: number;
    /** Gaps between the left group, the optional model slot, and the trailing cluster. */
    toolbarGaps: number;
    /** Trailing controls other than the shortcut sentence, including the gaps between them. */
    trailingFixedWidth: number;
    /** Extra gap paid to put the sentence among those controls. */
    hintGap: number;
    /** Intrinsic width of the sentence. */
    hintNaturalWidth: number;
}

/**
 * The left group is allowed to wrap and shrink. The sentence is the piece
 * that is not: it stays on the row only while the model slot and the send
 * cluster can keep their widths and still give the sentence its intrinsic
 * width. A viewport breakpoint cannot see the model name change length.
 * An unmeasured box (width 0) keeps the sentence: layout has not happened yet.
 */
export function composerShortcutHintFits(room: ComposerShortcutHintRoom): boolean {
    if (room.toolbarWidth <= 0 || room.hintNaturalWidth <= 0) return true;
    const occupied = room.endWidth
        + room.toolbarGap * room.toolbarGaps
        + room.trailingFixedWidth
        + room.hintGap
        + room.hintNaturalWidth;
    // Equality still fits. One more pixel of slack leaves the send control
    // past the row: the summed widths already sit about a pixel above the
    // painted edge.
    return occupied <= room.toolbarWidth;
}

function readGap(el: HTMLElement): number {
    const inline = el.style.columnGap || el.style.gap;
    const inlineValue = parseFloat(inline);
    if (inline && Number.isFinite(inlineValue)) return inlineValue;
    const computed = getComputedStyle(el);
    const raw = computed.columnGap && computed.columnGap !== "normal" ? computed.columnGap : computed.gap;
    const value = parseFloat(raw || "");
    return Number.isFinite(value) ? value : 0;
}

function horizontalPadding(el: HTMLElement): number {
    const inline = el.style.padding.trim();
    if (inline) {
        const parts = inline.split(/\s+/);
        const nums = parts.map(part => parseFloat(part));
        if (nums.length === parts.length && nums.every(Number.isFinite)) {
            if (nums.length === 1) return nums[0] * 2;
            if (nums.length === 2 || nums.length === 3) return nums[1] * 2;
            return nums[1] + nums[3];
        }
    }
    const style = getComputedStyle(el);
    return (parseFloat(style.paddingLeft || "") || 0) + (parseFloat(style.paddingRight || "") || 0);
}

function contentBoxWidth(el: HTMLElement): number {
    return Math.max(0, el.clientWidth - horizontalPadding(el));
}

function isOutOfFlow(el: HTMLElement): boolean {
    const position = el.style.position || getComputedStyle(el).position;
    return position === "absolute" || position === "fixed";
}

/** Sum in-flow children at their own widths. A stretched flex track is not the content. */
function sumInFlow(el: HTMLElement, gap: number, skip?: (child: HTMLElement) => boolean): { width: number; count: number } {
    let width = 0;
    let count = 0;
    for (const node of Array.from(el.children)) {
        const child = node as HTMLElement;
        if (skip?.(child) || isOutOfFlow(child)) continue;
        if (child.offsetWidth === 0 && child.offsetHeight === 0) continue;
        width += child.offsetWidth;
        count += 1;
    }
    if (count > 1) width += gap * (count - 1);
    return { width, count };
}

function isShortcutChrome(el: HTMLElement): boolean {
    return el.hasAttribute("data-shortcut-measure")
        || el.hasAttribute("data-shortcut-measure-host")
        || el.hasAttribute("data-shortcut-hint");
}

/** Read the live toolbar. The sentence's own box is not part of the reserved width. */
export function readComposerShortcutHintFit(toolbar: HTMLElement): boolean {
    const measure = toolbar.querySelector<HTMLElement>("[data-shortcut-measure]");
    const trailing = toolbar.querySelector<HTMLElement>("[data-toolbar-slot='trailing']");
    if (!measure || !trailing) return true;
    const end = toolbar.querySelector<HTMLElement>("[data-testid='ai-input-toolbar-end']");
    const trailingGap = readGap(trailing);
    const fixed = sumInFlow(trailing, trailingGap, isShortcutChrome);
    return composerShortcutHintFits({
        toolbarWidth: contentBoxWidth(toolbar),
        endWidth: end ? end.offsetWidth : 0,
        toolbarGap: readGap(toolbar),
        toolbarGaps: end ? 2 : 1,
        trailingFixedWidth: fixed.width,
        hintGap: fixed.count > 0 ? trailingGap : 0,
        hintNaturalWidth: measure.offsetWidth,
    });
}

/**
 * True while the sentence fits on the row.
 *
 * The toolbar's box changes when the composer is resized. The model slot and
 * the send cluster change size when their own content does, without resizing
 * the toolbar. The measure is clipped to an empty box, so a change in the
 * sentence's own width does not resize either of those. A model slot mounted
 * later is a direct child of the toolbar. Typing stays in the textarea,
 * outside this element, so it does not remeasure.
 */
export function useComposerShortcutHintVisible(toolbarRef: RefObject<HTMLElement | null>, shortcutHint: string): boolean {
    const [fits, setFits] = useState(true);
    const fitsRef = useRef(true);
    useLayoutEffect(() => {
        const toolbar = toolbarRef.current;
        if (!toolbar) return;
        let cancelled = false;
        const apply = () => {
            if (cancelled) return;
            const next = readComposerShortcutHintFit(toolbar);
            if (fitsRef.current === next) return;
            fitsRef.current = next;
            setFits(next);
        };
        const resize = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(apply);
        const bind = () => {
            if (!resize) return;
            resize.disconnect();
            resize.observe(toolbar);
            const end = toolbar.querySelector<HTMLElement>("[data-testid='ai-input-toolbar-end']");
            if (end) resize.observe(end);
            const trailing = toolbar.querySelector<HTMLElement>("[data-toolbar-slot='trailing']");
            if (trailing) resize.observe(trailing);
            const measure = toolbar.querySelector<HTMLElement>("[data-shortcut-measure]");
            if (measure) resize.observe(measure);
        };
        bind();
        apply();
        const mutations = typeof MutationObserver === "undefined" ? null : new MutationObserver(() => {
            bind();
            apply();
        });
        mutations?.observe(toolbar, { childList: true });
        return () => {
            cancelled = true;
            resize?.disconnect();
            mutations?.disconnect();
        };
    }, [shortcutHint, toolbarRef]);
    return fits;
}
