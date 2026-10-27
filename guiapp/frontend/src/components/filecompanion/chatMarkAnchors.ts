// Anchoring for companion chat highlights. A mark is stored as character
// offsets into one message's rendered text, so this module owns the mapping
// between a DOM selection and those offsets. Text inside controls and the
// reasoning panel is skipped: the offsets must describe the answer the reader
// highlighted, and expandable chrome would shift them when it opens or closes.
//
// Rendering uses the CSS Custom Highlight API: live DOM ranges paint the
// highlight without wrapping React's text nodes, so streaming and re-renders
// cannot corrupt the transcript.

import { CHAT_MARK_COLORS, type ChatMark, type ChatMarkColor } from "./chatMarks";

const SKIP_SELECTOR = "button, svg, script, style, noscript, template, [data-chat-mark-skip]";

export type ChatTextSpan = { node: Text; start: number; end: number };

export type ChatSelectionSlice = { message: number; start: number; end: number; text: string };

export function chatTextSpans(root: HTMLElement): ChatTextSpan[] {
    const spans: ChatTextSpan[] = [];
    if (!root || typeof document === "undefined") return spans;
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, {
        acceptNode(node) {
            const parent = (node as Text).parentElement;
            if (!parent || parent.closest(SKIP_SELECTOR)) return NodeFilter.FILTER_REJECT;
            return node.nodeValue ? NodeFilter.FILTER_ACCEPT : NodeFilter.FILTER_REJECT;
        },
    });
    let offset = 0;
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
        const text = node as Text;
        const length = text.nodeValue?.length ?? 0;
        if (length === 0) continue;
        spans.push({ node: text, start: offset, end: offset + length });
        offset += length;
    }
    return spans;
}

export function chatMessageText(spans: ChatTextSpan[]): string {
    let text = "";
    for (const span of spans) text += span.node.nodeValue ?? "";
    return text;
}

function collapsedRange(node: Node, offset: number): Range | null {
    const range = document.createRange();
    try {
        range.setStart(node, offset);
    } catch {
        return null;
    }
    range.collapse(true);
    return range;
}

/** -1 when the first point sits before the second, 0 on the same point, 1 after. */
function pointCompare(aNode: Node, aOffset: number, bNode: Node, bOffset: number): number {
    const left = collapsedRange(aNode, aOffset);
    const right = collapsedRange(bNode, bOffset);
    if (!left || !right) return 0;
    return left.compareBoundaryPoints(Range.START_TO_START, right);
}

function pointForOffset(spans: ChatTextSpan[], offset: number, edge: "start" | "end"): { node: Text; offset: number } | null {
    if (spans.length === 0) return null;
    if (edge === "start") {
        for (const span of spans) {
            if (span.end <= offset) continue;
            const within = Math.max(0, Math.min(offset - span.start, span.end - span.start));
            return { node: span.node, offset: within };
        }
        return null;
    }
    for (let index = spans.length - 1; index >= 0; index -= 1) {
        const span = spans[index];
        if (span.start >= offset) continue;
        const within = Math.max(0, Math.min(offset - span.start, span.end - span.start));
        return { node: span.node, offset: within };
    }
    return null;
}

/** Maps a DOM boundary point to a character offset inside the message text.
 *  A point in skipped chrome resolves to the nearest visible edge. */
export function chatOffsetForPoint(spans: ChatTextSpan[], node: Node, offset: number, edge: "start" | "end"): number {
    const total = spans.length > 0 ? spans[spans.length - 1].end : 0;
    const probe = collapsedRange(node, offset);
    if (!probe) return edge === "start" ? 0 : total;
    if (node.nodeType === Node.TEXT_NODE) {
        for (const span of spans) {
            if (span.node !== node) continue;
            return span.start + Math.max(0, Math.min(offset, span.end - span.start));
        }
    }
    if (edge === "start") {
        for (const span of spans) {
            const start = collapsedRange(span.node, 0);
            if (start && probe.compareBoundaryPoints(Range.START_TO_START, start) <= 0) return span.start;
        }
        return total;
    }
    let end = 0;
    for (const span of spans) {
        const spanEnd = collapsedRange(span.node, span.node.nodeValue?.length ?? 0);
        if (!spanEnd) continue;
        if (probe.compareBoundaryPoints(Range.START_TO_START, spanEnd) >= 0) end = span.end;
        else break;
    }
    return end;
}

function messageElement(root: HTMLElement, message: number): HTMLElement | null {
    return root.querySelector<HTMLElement>(`[data-chat-message="${message}"]`);
}

function messageElements(root: HTMLElement): HTMLElement[] {
    return Array.from(root.querySelectorAll<HTMLElement>("[data-chat-message]"));
}

// overlapOf clamps the selection to one message. The selection can start in an
// earlier bubble and end in a later one, so both ends are compared to the
// message's own boundaries before either is used.
function overlapOf(element: HTMLElement, range: Range): Range | null {
    const last = element.childNodes.length;
    const startsAfter = pointCompare(range.startContainer, range.startOffset, element, last) >= 0;
    const endsBefore = pointCompare(range.endContainer, range.endOffset, element, 0) <= 0;
    if (startsAfter || endsBefore) return null;
    const startsInside = pointCompare(range.startContainer, range.startOffset, element, 0) > 0;
    const endsInside = pointCompare(range.endContainer, range.endOffset, element, last) < 0;
    const startNode = startsInside ? range.startContainer : element;
    const startOffset = startsInside ? range.startOffset : 0;
    const endNode = endsInside ? range.endContainer : element;
    const endOffset = endsInside ? range.endOffset : last;
    if (pointCompare(startNode, startOffset, endNode, endOffset) >= 0) return null;
    const overlap = document.createRange();
    try {
        overlap.setStart(startNode, startOffset);
        overlap.setEnd(endNode, endOffset);
    } catch {
        return null;
    }
    return overlap;
}

/** One slice per message the selection covers, in transcript order. The
 *  offsets are narrowed to the non-blank core, so the stored range and the
 *  stored quote describe exactly the same characters. */
export function chatSelectionSlices(root: HTMLElement, range: Range): ChatSelectionSlice[] {
    const slices: ChatSelectionSlice[] = [];
    if (!root || !range) return slices;
    for (const element of messageElements(root)) {
        const message = Number(element.dataset.chatMessage);
        if (!Number.isFinite(message) || message < 0) continue;
        const overlap = overlapOf(element, range);
        if (!overlap) continue;
        const spans = chatTextSpans(element);
        if (spans.length === 0) continue;
        const start = chatOffsetForPoint(spans, overlap.startContainer, overlap.startOffset, "start");
        const end = chatOffsetForPoint(spans, overlap.endContainer, overlap.endOffset, "end");
        if (end <= start) continue;
        const raw = chatMessageText(spans).slice(start, end);
        const leading = raw.length - raw.trimStart().length;
        const text = raw.trim();
        if (!text) continue;
        slices.push({ message, start: start + leading, end: start + leading + text.length, text });
    }
    return slices;
}

/** The offsets to paint for a stored mark. Offsets are trusted while they
 *  still read the same text; a moved answer re-anchors on the quote, choosing
 *  the occurrence closest to where the mark used to be. */
function offsetsInText(text: string, mark: ChatMark): { start: number; end: number } | null {
    if (text.length === 0 || !mark.text) return null;
    if (mark.start >= 0 && mark.end <= text.length && mark.end > mark.start && text.slice(mark.start, mark.end) === mark.text) {
        return { start: mark.start, end: mark.end };
    }
    let best = -1;
    let bestDistance = Number.POSITIVE_INFINITY;
    for (let from = 0; ;) {
        const found = text.indexOf(mark.text, from);
        if (found < 0) break;
        const distance = Math.abs(found - mark.start);
        if (distance < bestDistance) {
            bestDistance = distance;
            best = found;
        }
        from = found + 1;
    }
    if (best < 0) return null;
    return { start: best, end: best + mark.text.length };
}

export function chatMarkOffsets(spans: ChatTextSpan[], mark: ChatMark): { start: number; end: number } | null {
    return offsetsInText(chatMessageText(spans), mark);
}

function rangeFromText(spans: ChatTextSpan[], text: string, mark: ChatMark): Range | null {
    const offsets = offsetsInText(text, mark);
    if (!offsets) return null;
    const start = pointForOffset(spans, offsets.start, "start");
    const end = pointForOffset(spans, offsets.end, "end");
    if (!start || !end) return null;
    const range = document.createRange();
    range.setStart(start.node, start.offset);
    range.setEnd(end.node, end.offset);
    return range;
}

export function chatRangeForMark(root: HTMLElement, mark: ChatMark): Range | null {
    const element = messageElement(root, mark.message);
    if (!element) return null;
    const spans = chatTextSpans(element);
    if (spans.length === 0) return null;
    return rangeFromText(spans, chatMessageText(spans), mark);
}

/** Which stored mark covers a point inside one message, if any. The mark is
 *  re-anchored first, because the painted range and the stored offsets can
 *  differ once the answer has been re-rendered. */
export function chatMarkAtOffset(spans: ChatTextSpan[], marks: ChatMark[], message: number, offset: number): number {
    if (spans.length === 0) return -1;
    let text = "";
    for (let index = marks.length - 1; index >= 0; index -= 1) {
        const mark = marks[index];
        if (mark.message !== message) continue;
        if (!text) text = chatMessageText(spans);
        const offsets = offsetsInText(text, mark);
        if (!offsets) continue;
        if (offset >= offsets.start && offset <= offsets.end) return index;
    }
    return -1;
}

export function chatMessageIndexAt(element: HTMLElement | null): number {
    const host = element?.closest<HTMLElement>("[data-chat-message]");
    if (!host) return -1;
    const index = Number(host.dataset.chatMessage);
    return Number.isFinite(index) ? index : -1;
}

export function chatMessageRoot(element: HTMLElement | null): HTMLElement | null {
    return element?.closest<HTMLElement>("[data-chat-message]") ?? null;
}

const highlightName = (color: ChatMarkColor): string => `companion-chat-${color}`;

function highlightRegistry(): HighlightRegistry | null {
    if (typeof CSS === "undefined" || !CSS) return null;
    const registry = (CSS as unknown as { highlights?: HighlightRegistry }).highlights;
    return registry && typeof registry.set === "function" ? registry : null;
}

export function chatHighlightsSupported(): boolean {
    return typeof Highlight === "function" && highlightRegistry() !== null;
}

/** Paints every mark that still anchors. Returns false when this WebView has
 *  no Custom Highlight API, so the caller can skip the work entirely. The
 *  text walk runs once per message, not once per mark. */
export function applyChatHighlights(root: HTMLElement, marks: ChatMark[]): boolean {
    const registry = highlightRegistry();
    if (!registry || typeof Highlight !== "function") return false;
    const byMessage = new Map<number, ChatMark[]>();
    for (const mark of marks) {
        const list = byMessage.get(mark.message);
        if (list) list.push(mark);
        else byMessage.set(mark.message, [mark]);
    }
    const buckets = new Map<ChatMarkColor, Range[]>();
    for (const [message, list] of byMessage) {
        const element = messageElement(root, message);
        if (!element) continue;
        const spans = chatTextSpans(element);
        if (spans.length === 0) continue;
        const text = chatMessageText(spans);
        for (const mark of list) {
            const range = rangeFromText(spans, text, mark);
            if (!range) continue;
            const bucket = buckets.get(mark.color);
            if (bucket) bucket.push(range);
            else buckets.set(mark.color, [range]);
        }
    }
    for (const color of CHAT_MARK_COLORS) {
        const name = highlightName(color.id);
        const ranges = buckets.get(color.id);
        if (!ranges || ranges.length === 0) {
            registry.delete(name);
            continue;
        }
        registry.set(name, new Highlight(...ranges));
    }
    return true;
}

export function clearChatHighlights(): void {
    const registry = highlightRegistry();
    if (!registry) return;
    for (const color of CHAT_MARK_COLORS) registry.delete(highlightName(color.id));
}
