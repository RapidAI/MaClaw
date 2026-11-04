// Document find stays in this window. Matches are drawn on the local page
// only — they are not marks, notes, drawings, or bookmarks, and nothing here
// is written for share participants.

export type FindSource = {
    text: string;
    /** Characters inserted after this run for matching, never highlighted. */
    gapAfter?: string;
};

export type FindPart = { run: number; start: number; end: number };

export type FindHit = { parts: FindPart[] };

type FoldSpan = {
    run: number;
    origin: number;
    foldedLength: number;
    textLength: number;
    /** Folded index → original UTF-16 index. Absent when folding keeps indexes aligned. */
    map: number[] | null;
};

export type FoldedText = { hay: string; spans: FoldSpan[] };

const COMBINING_MARKS = /\p{M}/gu;

function isUnicodeSpace(code: number): boolean {
    return code === 9 || code === 10 || code === 11 || code === 12 || code === 13 || code === 32
        || code === 0xa0 || code === 0x1680 || code === 0x2028 || code === 0x2029 || code === 0x202f
        || code === 0x205f || code === 0x3000 || (code >= 0x2000 && code <= 0x200a);
}

function isSkippable(code: number): boolean {
    return code === 0xad || code === 0x200b || code === 0x200c || code === 0x200d || code === 0x2060 || code === 0xfeff;
}

function unfoldFullwidth(code: number): number {
    if (code >= 0xff10 && code <= 0xff19) return code - 0xff10 + 0x30;
    if (code >= 0xff21 && code <= 0xff3a) return code - 0xff21 + 0x41;
    if (code >= 0xff41 && code <= 0xff5a) return code - 0xff41 + 0x61;
    return code;
}

/** Characters whose lower-case and NFD form are themselves. A-Z and accents are not. */
function isFoldStable(code: number): boolean {
    if (code <= 0x7f) return code < 65 || code > 90;
    if (code >= 0x3000 && code <= 0x303f) return true;
    if (code >= 0x3040 && code <= 0x30ff) return true;
    if (code >= 0x3100 && code <= 0x312f) return true;
    if (code >= 0x3400 && code <= 0x4dbf) return true;
    if (code >= 0x4e00 && code <= 0x9fff) return true;
    if (code >= 0xac00 && code <= 0xd7af) return true;
    if (code >= 0xf900 && code <= 0xfaff) return true;
    if (code >= 0xff01 && code <= 0xff20) return code < 0xff10 || code > 0xff19;
    if (code >= 0xff3b && code <= 0xff40) return true;
    if (code >= 0xff5b && code <= 0xffef) return true;
    return false;
}

function needsSlowFold(text: string): boolean {
    let previousSpace = false;
    for (let index = 0; index < text.length;) {
        const code = text.codePointAt(index) ?? 0;
        index += code > 0xffff ? 2 : 1;
        if (isSkippable(code)) return true;
        const space = isUnicodeSpace(code);
        if (space && (previousSpace || code !== 32)) return true;
        if (!space && code > 0x7f && !isFoldStable(code)) return true;
        previousSpace = space;
    }
    return false;
}

/**
 * Lower case, turn fullwidth letters and digits into ASCII, drop combining
 * marks and soft hyphens, and collapse every whitespace run to one space.
 */
function foldMapped(text: string): { folded: string; map: number[] | null } {
    if (!needsSlowFold(text)) return { folded: text.toLowerCase(), map: null };
    const folded: string[] = [];
    const map: number[] = [];
    let aligned = true;
    let index = 0;
    let inSpace = false;
    while (index < text.length) {
        const code = text.codePointAt(index) ?? 0;
        const raw = String.fromCodePoint(code);
        if (isSkippable(code)) {
            aligned = false;
            index += raw.length;
            continue;
        }
        if (isUnicodeSpace(code)) {
            if (!inSpace) {
                folded.push(" ");
                map.push(index);
                if (raw.length !== 1) aligned = false;
            } else {
                aligned = false;
            }
            inSpace = true;
            index += raw.length;
            continue;
        }
        inSpace = false;
        const foldedCode = unfoldFullwidth(code);
        const unit = foldedCode === code ? raw : String.fromCodePoint(foldedCode);
        const next = foldedCode <= 0x7f
            ? unit.toLowerCase()
            : unit.toLowerCase().normalize("NFD").replace(COMBINING_MARKS, "");
        if (next.length !== raw.length) aligned = false;
        for (let i = 0; i < next.length; i += 1) map.push(index);
        if (next) folded.push(next);
        index += raw.length;
    }
    const joined = folded.join("");
    if (aligned && joined.length === text.length) return { folded: joined, map: null };
    return { folded: joined, map };
}

/** The hay already ended with a space, so a leading space in this run is a duplicate. */
function dropLeadingSpace(folded: string, map: number[] | null): { folded: string; map: number[] | null } | null {
    if (!folded.startsWith(" ")) return { folded, map };
    const next = folded.slice(1);
    if (!next) return null;
    if (map) return { folded: next, map: map.slice(1) };
    const shifted: number[] = [];
    for (let index = 1; index < folded.length; index += 1) shifted.push(index);
    return { folded: next, map: shifted };
}

function originalEnd(span: FoldSpan, localEnd: number): number {
    if (localEnd <= 0) return 0;
    // Aligned folding keeps folded indexes identical to the original text.
    if (!span.map) return Math.min(span.textLength, localEnd);
    if (localEnd >= span.map.length) return span.textLength;
    const last = span.map[localEnd - 1];
    let cursor = localEnd;
    while (cursor < span.map.length && span.map[cursor] === last) cursor += 1;
    return cursor >= span.map.length ? span.textLength : span.map[cursor];
}

function firstSpanAt(spans: readonly FoldSpan[], index: number): number {
    let lo = 0;
    let hi = spans.length - 1;
    let found = -1;
    while (lo <= hi) {
        const mid = (lo + hi) >> 1;
        if (spans[mid].origin <= index) {
            found = mid;
            lo = mid + 1;
        } else {
            hi = mid - 1;
        }
    }
    return found;
}

/** A line break or a block boundary is a space, so a wrapped phrase still matches. */
function appendSeparator(hay: string, gap: string, nextText: string): string {
    if (!gap) return hay;
    if (/^\s+$/u.test(gap)) {
        if (/\s$/u.test(hay) || /^\s/u.test(nextText)) return hay;
        return `${hay} `;
    }
    return hay + foldMapped(gap).folded;
}

export function foldFindSources(runs: readonly FindSource[]): FoldedText {
    const spans: FoldSpan[] = [];
    let hay = "";
    for (let index = 0; index < runs.length; index += 1) {
        const text = runs[index]?.text ?? "";
        if (text) {
            const mapped = foldMapped(text);
            const folded = hay.endsWith(" ") ? dropLeadingSpace(mapped.folded, mapped.map) : mapped;
            if (folded && folded.folded) {
                spans.push({
                    run: index,
                    origin: hay.length,
                    foldedLength: folded.folded.length,
                    textLength: text.length,
                    map: folded.map,
                });
                hay += folded.folded;
            }
        }
        hay = appendSeparator(hay, runs[index]?.gapAfter ?? "", runs[index + 1]?.text ?? "");
    }
    return { hay, spans };
}

export function findInFolded(page: FoldedText, query: string): FindHit[] {
    const needle = foldMapped(query.trim()).folded;
    if (!needle) return [];
    const hits: FindHit[] = [];
    let from = 0;
    while (from <= page.hay.length - needle.length) {
        const at = page.hay.indexOf(needle, from);
        if (at < 0) break;
        const end = at + needle.length;
        const parts: FindPart[] = [];
        let cursor = firstSpanAt(page.spans, at);
        if (cursor < 0) cursor = 0;
        for (let i = cursor; i < page.spans.length; i += 1) {
            const span = page.spans[i];
            if (span.origin >= end) break;
            const spanEnd = span.origin + span.foldedLength;
            if (spanEnd <= at) continue;
            const localStart = Math.max(0, at - span.origin);
            const localEnd = Math.min(span.foldedLength, end - span.origin);
            const start = span.map ? span.map[localStart] ?? 0 : localStart;
            const stop = originalEnd(span, localEnd);
            if (stop > start) parts.push({ run: span.run, start, end: stop });
        }
        if (parts.length) hits.push({ parts });
        from = end;
    }
    return hits;
}

/** Case-insensitive matches. A hit may cover several runs; gaps are searchable but not painted. */
export function findTextHits(runs: readonly FindSource[], query: string): FindHit[] {
    return findInFolded(foldFindSources(runs), query);
}

function glyphWeight(code: number): number {
    if (code > 0x7f) return 1;
    if (code === 32 || code === 9) return 0.33;
    const narrow = code === 105 || code === 108 || code === 106 || code === 116 || code === 102 || code === 114 || code === 73;
    if (narrow || code === 46 || code === 44 || code === 39 || code === 124) return 0.4;
    if (code === 109 || code === 119 || code === 77 || code === 87) return 1.2;
    return 0.7;
}

/** Where a slice sits inside a run. CJK stays equal-width; narrow Latin letters take less of the box. */
export function findSlice(text: string, start: number, end: number): { lead: number; slice: number } {
    if (!text || end <= start) return { lead: 0, slice: 0 };
    let before = 0;
    let inside = 0;
    let total = 0;
    for (let index = 0; index < text.length;) {
        const code = text.codePointAt(index) ?? 0;
        const size = code > 0xffff ? 2 : 1;
        const weight = glyphWeight(code);
        const from = index;
        const to = index + size;
        if (to <= start) before += weight;
        else if (from >= end) {
            // Past the slice.
        } else {
            const share = weight * (Math.min(end, to) - Math.max(start, from)) / size;
            if (from < start) before += weight - share;
            inside += share;
        }
        total += weight;
        index = to;
    }
    if (!(total > 0)) return { lead: start / text.length, slice: (end - start) / text.length };
    return { lead: before / total, slice: inside / total };
}

type PdfFindItem = { str?: string; hasEOL?: boolean; transform?: number[]; width?: number };

function pdfItemGap(item: PdfFindItem, next: PdfFindItem | undefined): string {
    if (!next) return "";
    if (item.hasEOL) return "\n";
    const from = item.transform;
    const to = next.transform;
    if (!from || !to || from.length < 6 || to.length < 6) return "";
    let vx = from[0];
    let vy = from[1];
    let scale = Math.hypot(vx, vy);
    if (!(scale > 0)) {
        vx = from[2];
        vy = from[3];
        scale = Math.hypot(vx, vy);
    }
    if (!(scale > 0)) return "";
    const ux = vx / scale;
    const uy = vy / scale;
    const endX = from[4] + ux * (item.width || 0);
    const endY = from[5] + uy * (item.width || 0);
    const dx = to[4] - endX;
    const dy = to[5] - endY;
    const along = dx * ux + dy * uy;
    const cross = dx * -uy + dy * ux;
    const fontHeight = Math.hypot(from[2], from[3]) || scale;
    if (Math.abs(cross) > fontHeight * 0.8) return "\n";
    return along > fontHeight * 0.25 ? " " : "";
}

export function pdfFindSources(items: readonly PdfFindItem[]): FindSource[] {
    return items.map((item, index) => ({
        text: String(item.str ?? ""),
        gapAfter: pdfItemGap(item, items[index + 1]),
    }));
}

const FIND_MARK = "companion-preview-find";
const BLOCK_SELECTOR = "p, li, td, th, h1, h2, h3, h4, h5, h6, pre, blockquote, dt, dd, figcaption, div, section, article, header, footer, tr";

export type TextRun = { node: Text; text: string; gapAfter?: string };

function owningBlock(node: Text): Element | null {
    return node.parentElement?.closest(BLOCK_SELECTOR) ?? null;
}

const SKIP_FIND_TAGS = "script, style, noscript, textarea, input, button";

export function collectFindSources(root: HTMLElement): TextRun[] {
    const doc = root.ownerDocument;
    if (!doc) return [];
    const walker = doc.createTreeWalker(root, NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT, {
        acceptNode(node) {
            const parent = node.parentElement;
            if (parent?.closest(SKIP_FIND_TAGS) || parent?.closest(`mark.${FIND_MARK}`)) return NodeFilter.FILTER_REJECT;
            if (node.nodeType === Node.ELEMENT_NODE) {
                const tag = (node as Element).tagName;
                if (tag === "SCRIPT" || tag === "STYLE" || tag === "NOSCRIPT" || tag === "TEXTAREA" || tag === "INPUT" || tag === "BUTTON") {
                    return NodeFilter.FILTER_REJECT;
                }
            }
            return NodeFilter.FILTER_ACCEPT;
        },
    });
    const runs: TextRun[] = [];
    let pendingBreak = false;
    let lastBlock: Element | null = null;
    let current = walker.nextNode();
    while (current) {
        if (current.nodeType === Node.ELEMENT_NODE) {
            if ((current as Element).tagName === "BR") pendingBreak = true;
            current = walker.nextNode();
            continue;
        }
        const node = current as Text;
        const text = node.data;
        if (text) {
            const block = owningBlock(node);
            const previous = runs[runs.length - 1];
            if (previous && (pendingBreak || lastBlock !== block)) previous.gapAfter = "\n";
            runs.push({ node, text });
            lastBlock = block;
            pendingBreak = false;
        }
        current = walker.nextNode();
    }
    return runs;
}

export type DomFindMark = { node: Text; start: number; end: number; active: boolean; index: number };

export function clearDomFindMarks(root: HTMLElement): void {
    const marks = root.querySelectorAll(`mark.${FIND_MARK}`);
    const parents = new Set<Node>();
    marks.forEach((mark) => {
        const parent = mark.parentNode;
        if (!parent) return;
        while (mark.firstChild) parent.insertBefore(mark.firstChild, mark);
        parent.removeChild(mark);
        parents.add(parent);
    });
    parents.forEach((parent) => parent.normalize());
}

/** Paint later slices of a text node first so earlier offsets stay valid. */
export function paintDomFindMarks(marks: readonly DomFindMark[]): void {
    const groups = new Map<Text, DomFindMark[]>();
    for (const mark of marks) {
        const list = groups.get(mark.node) ?? [];
        list.push(mark);
        groups.set(mark.node, list);
    }
    for (const list of groups.values()) {
        list.sort((a, b) => b.start - a.start);
        for (const mark of list) {
            const node = mark.node;
            if (!node.parentNode || mark.end <= mark.start || mark.end > node.data.length) continue;
            const range = node.ownerDocument.createRange();
            range.setStart(node, mark.start);
            range.setEnd(node, mark.end);
            const el = node.ownerDocument.createElement("mark");
            el.className = FIND_MARK;
            el.dataset.findHit = "true";
            el.dataset.findIndex = String(mark.index);
            if (mark.active) el.dataset.findActive = "true";
            try {
                range.surroundContents(el);
            } catch {
                range.detach?.();
            }
        }
    }
}

/** Move the active styling onto one hit. Marks stay put, so stepping does not rebuild the page. */
export function activateDomFindMark(root: HTMLElement, index: number): HTMLElement | null {
    let first: HTMLElement | null = null;
    const wanted = String(index);
    root.querySelectorAll<HTMLElement>(`mark.${FIND_MARK}`).forEach((mark) => {
        if (mark.dataset.findIndex === wanted) {
            mark.dataset.findActive = "true";
            if (!first) first = mark;
        } else {
            delete mark.dataset.findActive;
        }
    });
    return first;
}

/** Move only this scroller. scrollIntoView also scrolls the companion window. */
export function scrollMatchIntoScroller(scroller: HTMLElement, target: HTMLElement): void {
    const rect = target.getBoundingClientRect();
    const box = scroller.getBoundingClientRect();
    if (rect.height < 1 && rect.width < 1) return;
    const viewTop = box.top;
    const viewBottom = box.top + scroller.clientHeight;
    if (rect.top >= viewTop - 1 && rect.bottom <= viewBottom + 1) return;
    const delta = rect.top - viewTop - Math.max(0, (scroller.clientHeight - rect.height) / 2);
    if (Math.abs(delta) < 1) return;
    scroller.scrollTop += delta;
}
