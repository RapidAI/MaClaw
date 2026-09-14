/**
 * Pure helpers for the code-preview document minimap (thumbnail + page locator).
 * Kept free of React so unit tests can exercise mapping without mounting the panel.
 *
 * Named *Helpers.ts so it does not collide with CodePreviewMinimap.tsx on
 * case-insensitive filesystems (Windows).
 */

export const CODE_PREVIEW_MINIMAP_WIDTH = 64;
export const CODE_PREVIEW_MINIMAP_MIN_THUMB_PX = 18;
/** Draw scaled source text when each line gets at least this many CSS pixels. */
export const CODE_PREVIEW_MINIMAP_TEXT_ROW_PX = 2;
/** Cap painted rows so a 50k-line file does not allocate 50k sample objects. */
export const CODE_PREVIEW_MINIMAP_MAX_SAMPLES = 2048;

export type MinimapSampleKind = 'plain' | 'add' | 'delete' | 'match' | 'active-match';

export interface MinimapSample {
    indent: number;
    density: number;
    kind: MinimapSampleKind;
}

export interface MinimapThumbMetrics {
    top: number;
    height: number;
    maxScroll: number;
    travel: number;
}

export interface MinimapColors {
    text: string;
    add: string;
    delete: string;
    match: string;
    activeMatch: string;
}

export interface MinimapDiffLine {
    type: 'add' | 'delete' | 'unchanged';
    content: string;
    newLineNum?: number;
}

const KIND_RANK: Record<MinimapSampleKind, number> = {
    'active-match': 4,
    match: 3,
    add: 2,
    delete: 2,
    plain: 1,
};

const EMPTY_MATCH_SET: ReadonlySet<number> = new Set();

/** Leading whitespace width in space-equivalents (tab = 4). */
export function leadingWhitespaceColumns(line: string): number {
    let cols = 0;
    for (let i = 0; i < line.length; i++) {
        const ch = line.charCodeAt(i);
        if (ch === 32) cols += 1;
        else if (ch === 9) cols += 4;
        else break;
    }
    return cols;
}

export function sampleMinimapLine(line: string): Pick<MinimapSample, 'indent' | 'density'> {
    // One pass — avoid String#trim allocations on huge files.
    let i = 0;
    let indentCols = 0;
    for (; i < line.length; i++) {
        const ch = line.charCodeAt(i);
        if (ch === 32) indentCols += 1;
        else if (ch === 9) indentCols += 4;
        else break;
    }
    let end = line.length;
    while (end > i) {
        const ch = line.charCodeAt(end - 1);
        if (ch === 32 || ch === 9 || ch === 13) end -= 1;
        else break;
    }
    return {
        indent: Math.min(1, indentCols / 32),
        density: Math.min(1, (end - i) / 80),
    };
}

function kindForLine(lineIdx: number, matchSet: ReadonlySet<number>, activeMatchLine: number): MinimapSampleKind {
    if (lineIdx < 0) return 'plain';
    if (lineIdx === activeMatchLine) return 'active-match';
    if (matchSet.has(lineIdx)) return 'match';
    return 'plain';
}

function sampleAt(
    index: number,
    lines: string[],
    diffLines: MinimapDiffLine[] | null | undefined,
    matchSet: ReadonlySet<number>,
    active: number,
): MinimapSample {
    if (diffLines && diffLines.length > 0) {
        const dl = diffLines[index];
        if (!dl) return { indent: 0, density: 0, kind: 'plain' };
        const shape = sampleMinimapLine(dl.content);
        const newIdx = dl.newLineNum != null ? dl.newLineNum - 1 : -1;
        let kind: MinimapSampleKind = kindForLine(newIdx, matchSet, active);
        if (kind === 'plain') {
            if (dl.type === 'add') kind = 'add';
            else if (dl.type === 'delete') kind = 'delete';
        }
        return { ...shape, kind };
    }
    return {
        ...sampleMinimapLine(lines[index] ?? ''),
        kind: kindForLine(index, matchSet, active),
    };
}

function mergeKind(a: MinimapSampleKind, b: MinimapSampleKind): MinimapSampleKind {
    return KIND_RANK[a] >= KIND_RANK[b] ? a : b;
}

/** Bucket that contains `lineIdx` in a `bucketCount`-wide downsample of `sourceCount` rows. */
export function minimapBucketIndex(lineIdx: number, sourceCount: number, bucketCount: number): number {
    if (bucketCount <= 1 || sourceCount <= 0) return 0;
    if (lineIdx <= 0) return 0;
    if (lineIdx >= sourceCount) return bucketCount - 1;
    return Math.min(bucketCount - 1, Math.floor((lineIdx * bucketCount) / sourceCount));
}

function bucketSamples(
    sourceCount: number,
    maxRows: number,
    take: (index: number) => MinimapSample,
): MinimapSample[] {
    const out: MinimapSample[] = new Array(maxRows);
    for (let i = 0; i < maxRows; i++) {
        const start = Math.floor((i * sourceCount) / maxRows);
        const end = Math.max(start + 1, Math.floor(((i + 1) * sourceCount) / maxRows));
        let indent = 0;
        let density = 0;
        let kind: MinimapSampleKind = 'plain';
        let count = 0;
        for (let j = start; j < end && j < sourceCount; j++) {
            const s = take(j);
            indent += s.indent;
            density = Math.max(density, s.density);
            kind = mergeKind(kind, s.kind);
            count += 1;
        }
        out[i] = {
            indent: count > 0 ? indent / count : 0,
            density,
            kind,
        };
    }
    return out;
}

/**
 * One sample per visual row (or a capped bucket list for huge files).
 * Diff rows keep add/delete color; find matches overlay on new-file line numbers.
 */
export function buildMinimapSamples(opts: {
    lines: string[];
    diffLines?: MinimapDiffLine[] | null;
    matchLineIndexes?: readonly number[];
    activeMatchLine?: number;
}): MinimapSample[] {
    const matchList = opts.matchLineIndexes;
    const matchSet = matchList && matchList.length > 0 ? new Set(matchList) : EMPTY_MATCH_SET;
    const active = opts.activeMatchLine ?? -1;
    const diffLines = opts.diffLines;
    const sourceCount = diffLines && diffLines.length > 0 ? diffLines.length : opts.lines.length;
    if (sourceCount <= 0) return [];
    const take = (index: number) => sampleAt(index, opts.lines, diffLines, matchSet, active);
    if (sourceCount > CODE_PREVIEW_MINIMAP_MAX_SAMPLES) {
        // Plain logs: one representative row per bucket (O(buckets)) plus O(matches)
        // stamps. Diff views keep a full merge so add/delete color stays accurate.
        if (diffLines && diffLines.length > 0) {
            return bucketSamples(sourceCount, CODE_PREVIEW_MINIMAP_MAX_SAMPLES, take);
        }
        const maxRows = CODE_PREVIEW_MINIMAP_MAX_SAMPLES;
        const out: MinimapSample[] = new Array(maxRows);
        for (let i = 0; i < maxRows; i++) {
            const start = Math.floor((i * sourceCount) / maxRows);
            const end = Math.max(start + 1, Math.floor(((i + 1) * sourceCount) / maxRows));
            const mid = Math.min(sourceCount - 1, start + ((end - start) >> 1));
            out[i] = take(mid);
        }
        if (matchList && matchList.length > 0) {
            for (let k = 0; k < matchList.length; k++) {
                const lineIdx = matchList[k];
                if (lineIdx < 0 || lineIdx >= sourceCount) continue;
                const bucket = minimapBucketIndex(lineIdx, sourceCount, maxRows);
                const prev = out[bucket];
                if (KIND_RANK[prev.kind] < KIND_RANK.match) {
                    out[bucket] = { indent: prev.indent, density: Math.max(prev.density, 0.2), kind: 'match' };
                }
            }
        }
        return out;
    }
    const out: MinimapSample[] = new Array(sourceCount);
    for (let i = 0; i < sourceCount; i++) out[i] = take(i);
    return out;
}

/** Collapse N samples into at most `maxRows` buckets (long files). */
export function downsampleMinimapSamples(samples: MinimapSample[], maxRows: number): MinimapSample[] {
    if (maxRows <= 0 || samples.length === 0) return [];
    if (samples.length <= maxRows) return samples;
    return bucketSamples(samples.length, maxRows, (index) => samples[index]);
}

export function minimapTrackMetrics(
    clientHeight: number,
    scrollHeight: number,
    trackHeight: number,
    minThumbPx: number = CODE_PREVIEW_MINIMAP_MIN_THUMB_PX,
): Pick<MinimapThumbMetrics, 'height' | 'maxScroll' | 'travel'> {
    const maxScroll = Math.max(0, scrollHeight - clientHeight);
    const viewRatio = scrollHeight <= 0 ? 1 : Math.min(1, clientHeight / Math.max(scrollHeight, 1));
    const height = Math.min(trackHeight, Math.max(minThumbPx, viewRatio * trackHeight));
    const travel = Math.max(0, trackHeight - height);
    return { height, maxScroll, travel };
}

export function minimapThumbFromScroll(
    scrollTop: number,
    clientHeight: number,
    scrollHeight: number,
    trackHeight: number,
    minThumbPx: number = CODE_PREVIEW_MINIMAP_MIN_THUMB_PX,
): MinimapThumbMetrics {
    const { height, maxScroll, travel } = minimapTrackMetrics(
        clientHeight,
        scrollHeight,
        trackHeight,
        minThumbPx,
    );
    const top = maxScroll <= 0 || travel <= 0
        ? 0
        : (Math.max(0, Math.min(maxScroll, scrollTop)) / maxScroll) * travel;
    return { top, height, maxScroll, travel };
}

export function minimapThumbTopToScrollTop(thumbTop: number, travel: number, maxScroll: number): number {
    if (travel <= 0 || maxScroll <= 0) return 0;
    const t = Math.max(0, Math.min(1, thumbTop / travel));
    return t * maxScroll;
}

/**
 * Map a pointer Y (track-relative) to scrollTop.
 * `grabOffsetInThumb` is where the pointer grabbed the thumb (Apple: keep that offset).
 */
export function minimapPointerToScrollTop(
    y: number,
    trackHeight: number,
    clientHeight: number,
    scrollHeight: number,
    grabOffsetInThumb: number,
    minThumbPx: number = CODE_PREVIEW_MINIMAP_MIN_THUMB_PX,
): number {
    const { maxScroll, travel } = minimapTrackMetrics(
        clientHeight,
        scrollHeight,
        trackHeight,
        minThumbPx,
    );
    return minimapThumbTopToScrollTop(y - grabOffsetInThumb, travel, maxScroll);
}

export function clampMinimapScrollTop(scrollTop: number, clientHeight: number, scrollHeight: number): number {
    const maxScroll = Math.max(0, scrollHeight - clientHeight);
    if (!Number.isFinite(scrollTop)) return 0;
    return Math.max(0, Math.min(maxScroll, scrollTop));
}

/** WheelEvent.deltaMode: 0 = pixels, 1 = lines, 2 = pages. */
export function minimapWheelDeltaPixels(deltaY: number, deltaMode: number, clientHeight: number): number {
    if (deltaMode === 1) return deltaY * 16;
    if (deltaMode === 2) return deltaY * Math.max(1, clientHeight);
    return deltaY;
}

export function minimapThumbsEqual(a: MinimapThumbMetrics, b: MinimapThumbMetrics): boolean {
    return a.top === b.top && a.height === b.height && a.maxScroll === b.maxScroll && a.travel === b.travel;
}

/**
 * Decide which scroll-content node a ResizeObserver should follow.
 * Returns the node to unobserve, the node to observe, and the stored next value.
 */
export function nextObservedScrollContent(
    current: Element | null,
    firstElementChild: Element | null,
): { unobserve: Element | null; observe: Element | null; next: Element | null } {
    if (firstElementChild === current) {
        return { unobserve: null, observe: null, next: current };
    }
    return { unobserve: current, observe: firstElementChild, next: firstElementChild };
}

/**
 * Vertical ratio (0–1) of a 0-based new-file line on the minimap track.
 * Diff rows use the visual row that carries that new-file number.
 */
export function minimapMarkerRatio(
    activeMatchLine: number,
    lineCount: number,
    diffLines?: MinimapDiffLine[] | null,
): number | null {
    if (activeMatchLine < 0) return null;
    if (diffLines && diffLines.length > 0) {
        for (let i = 0; i < diffLines.length; i++) {
            if (diffLines[i].newLineNum === activeMatchLine + 1) {
                return (i + 0.5) / diffLines.length;
            }
        }
        return null;
    }
    if (lineCount <= 0 || activeMatchLine >= lineCount) return null;
    return (activeMatchLine + 0.5) / lineCount;
}

export function colorForMinimapKind(kind: MinimapSampleKind, colors: MinimapColors): string {
    switch (kind) {
        case 'add': return colors.add;
        case 'delete': return colors.delete;
        case 'match': return colors.match;
        case 'active-match': return colors.activeMatch;
        default: return colors.text;
    }
}

export function shouldRenderMinimapText(lineCount: number, canvasHeight: number): boolean {
    if (lineCount <= 0 || canvasHeight <= 0) return false;
    return canvasHeight / lineCount >= CODE_PREVIEW_MINIMAP_TEXT_ROW_PX;
}

function lineHasVisibleGlyphs(line: string): boolean {
    for (let i = 0; i < line.length; i++) {
        const ch = line.charCodeAt(i);
        if (ch !== 32 && ch !== 9 && ch !== 13) return true;
    }
    return false;
}

type MinimapDrawContext = Pick<CanvasRenderingContext2D, 'clearRect' | 'fillRect' | 'fillText' | 'save' | 'restore'> & {
    globalAlpha: number;
    fillStyle: string | CanvasGradient | CanvasPattern;
    font: string;
    textBaseline: CanvasTextBaseline;
};

/**
 * Paint the document thumbnail. Short files render scaled source text;
 * long files downsample to density bars (indent + length).
 */
export function drawCodePreviewMinimap(
    ctx: MinimapDrawContext,
    opts: {
        width: number;
        height: number;
        samples: MinimapSample[];
        lines: string[];
        colors: MinimapColors;
    },
): void {
    const { width, height, samples, lines, colors } = opts;
    ctx.clearRect(0, 0, width, height);
    if (width <= 0 || height <= 0 || samples.length === 0) return;

    const padX = 3;
    const innerW = Math.max(1, width - padX * 2);

    // Text mode is 1:1 with source rows. Bucketed samples must not paint the
    // first N source lines as if they were the whole file.
    if (
        shouldRenderMinimapText(samples.length, height)
        && lines.length === samples.length
    ) {
        const rowH = height / samples.length;
        const fontPx = Math.max(CODE_PREVIEW_MINIMAP_TEXT_ROW_PX, Math.min(4.5, rowH * 0.95));
        ctx.font = `${fontPx}px ui-monospace, SFMono-Regular, Menlo, Consolas, monospace`;
        ctx.textBaseline = 'top';
        for (let i = 0; i < samples.length; i++) {
            const line = lines[i];
            if (!line || !lineHasVisibleGlyphs(line)) continue;
            ctx.fillStyle = colorForMinimapKind(samples[i].kind, colors);
            ctx.globalAlpha = samples[i].kind === 'plain' ? 0.55 : 0.92;
            const y = i * rowH;
            ctx.fillText(line.replace(/\t/g, '  ').slice(0, 48), padX, y, innerW);
        }
        ctx.globalAlpha = 1;
        return;
    }

    const drawn = downsampleMinimapSamples(samples, Math.max(1, Math.round(height)));
    const rowH = height / drawn.length;
    for (let i = 0; i < drawn.length; i++) {
        const s = drawn[i];
        if (s.density <= 0.02) continue;
        const y = i * rowH;
        const h = Math.max(1, rowH * 0.72);
        const x = padX + s.indent * innerW * 0.35;
        const w = Math.max(2, s.density * (width - x - padX));
        ctx.fillStyle = colorForMinimapKind(s.kind, colors);
        ctx.globalAlpha = s.kind === 'plain' ? 0.42 : 0.88;
        ctx.fillRect(x, y, w, h);
    }
    ctx.globalAlpha = 1;
}
