import { describe, expect, it } from 'vitest';
import {
    CODE_PREVIEW_MINIMAP_MAX_SAMPLES,
    buildMinimapSamples,
    clampMinimapScrollTop,
    colorForMinimapKind,
    downsampleMinimapSamples,
    drawCodePreviewMinimap,
    leadingWhitespaceColumns,
    minimapPointerToScrollTop,
    minimapThumbFromScroll,
    minimapBucketIndex,
    minimapMarkerRatio,
    minimapThumbTopToScrollTop,
    minimapThumbsEqual,
    minimapWheelDeltaPixels,
    nextObservedScrollContent,
    sampleMinimapLine,
    shouldRenderMinimapText,
    type MinimapSample,
} from '../codePreviewMinimapHelpers';

const colors = {
    text: '#888',
    add: '#3a6',
    delete: '#c43',
    match: '#eab308',
    activeMatch: '#facc15',
};

function sample(partial: Partial<MinimapSample> & { density?: number }): MinimapSample {
    return {
        indent: 0,
        density: 0.5,
        kind: 'plain',
        ...partial,
    };
}

describe('codePreviewMinimap helpers', () => {
    it('counts leading whitespace in space-equivalents', () => {
        expect(leadingWhitespaceColumns('foo')).toBe(0);
        expect(leadingWhitespaceColumns('  foo')).toBe(2);
        expect(leadingWhitespaceColumns('\tfoo')).toBe(4);
        expect(leadingWhitespaceColumns('\t  foo')).toBe(6);
    });

    it('samples indent and density from a source line', () => {
        const empty = sampleMinimapLine('');
        expect(empty.density).toBe(0);
        const indented = sampleMinimapLine('        ' + 'x'.repeat(40));
        expect(indented.indent).toBeGreaterThan(0);
        expect(indented.density).toBeGreaterThan(0.4);
        expect(indented.density).toBeLessThanOrEqual(1);
        // Indent must not inflate density — otherwise padded short lines look "full".
        const padded = sampleMinimapLine('        x');
        expect(padded.indent).toBeGreaterThan(0);
        expect(padded.density).toBe(1 / 80);
    });

    it('builds samples for plain lines and overlays find matches', () => {
        const samples = buildMinimapSamples({
            lines: ['a', 'b match', 'c'],
            matchLineIndexes: [1],
            activeMatchLine: 1,
        });
        expect(samples).toHaveLength(3);
        expect(samples[0].kind).toBe('plain');
        expect(samples[1].kind).toBe('active-match');
        expect(samples[2].kind).toBe('plain');
    });

    it('uses diff rows and colors add/delete unless a find match wins', () => {
        const samples = buildMinimapSamples({
            lines: ['keep', 'added'],
            diffLines: [
                { type: 'unchanged', content: 'keep', newLineNum: 1 },
                { type: 'delete', content: 'gone' },
                { type: 'add', content: 'added', newLineNum: 2 },
            ],
            matchLineIndexes: [1],
            activeMatchLine: -1,
        });
        expect(samples.map((s) => s.kind)).toEqual(['plain', 'delete', 'match']);
    });

    it('downsamples long files and keeps the strongest kind in a bucket', () => {
        const samples = [
            sample({ kind: 'plain', density: 0.2 }),
            sample({ kind: 'add', density: 0.9 }),
            sample({ kind: 'plain', density: 0.1 }),
            sample({ kind: 'match', density: 0.3 }),
        ];
        const down = downsampleMinimapSamples(samples, 2);
        expect(down).toHaveLength(2);
        expect(down[0].kind).toBe('add');
        expect(down[0].density).toBe(0.9);
        expect(down[1].kind).toBe('match');
        expect(downsampleMinimapSamples(samples, 10)).toHaveLength(4);
        expect(downsampleMinimapSamples([], 8)).toEqual([]);
    });

    it('caps huge files instead of allocating one sample per line', () => {
        const lines = Array.from({ length: CODE_PREVIEW_MINIMAP_MAX_SAMPLES + 50 }, (_, i) => `l${i} ${'x'.repeat(12)}`);
        const samples = buildMinimapSamples({ lines });
        expect(samples).toHaveLength(CODE_PREVIEW_MINIMAP_MAX_SAMPLES);
        expect(samples.some((s) => s.density > 0)).toBe(true);
    });

    it('stamps find matches onto capped buckets without a per-line merge', () => {
        const n = CODE_PREVIEW_MINIMAP_MAX_SAMPLES + 200;
        const hit = n - 3;
        const lines = Array.from({ length: n }, (_, i) => (i === hit ? 'needle' : ''));
        const samples = buildMinimapSamples({ lines, matchLineIndexes: [hit] });
        const bucket = minimapBucketIndex(hit, n, CODE_PREVIEW_MINIMAP_MAX_SAMPLES);
        expect(samples[bucket].kind).toBe('match');
        expect(minimapBucketIndex(0, 100, 10)).toBe(0);
        expect(minimapBucketIndex(9, 100, 10)).toBe(0);
        expect(minimapBucketIndex(10, 100, 10)).toBe(1);
        expect(minimapBucketIndex(99, 100, 10)).toBe(9);
    });

    it('maps the viewport thumb from scroll metrics, inflating a tiny page', () => {
        const full = minimapThumbFromScroll(0, 200, 200, 100, 18);
        expect(full.maxScroll).toBe(0);
        expect(full.height).toBe(100);
        expect(full.top).toBe(0);

        const mid = minimapThumbFromScroll(400, 200, 1000, 100, 18);
        expect(mid.maxScroll).toBe(800);
        expect(mid.height).toBe(20);
        expect(mid.top).toBe(40);

        const tinyPage = minimapThumbFromScroll(0, 20, 2000, 100, 18);
        expect(tinyPage.height).toBe(18);
        expect(tinyPage.travel).toBe(82);
    });

    it('converts thumb position and pointer Y to scrollTop', () => {
        expect(minimapThumbTopToScrollTop(40, 80, 800)).toBe(400);
        expect(minimapThumbTopToScrollTop(-10, 80, 800)).toBe(0);
        expect(minimapThumbTopToScrollTop(90, 80, 800)).toBe(800);
        expect(minimapThumbTopToScrollTop(10, 0, 800)).toBe(0);

        // Click the track with grabOffset = thumbHeight/2 (page locate).
        const y = 60;
        const scroll = minimapPointerToScrollTop(y, 100, 200, 1000, 10, 18);
        expect(scroll).toBeGreaterThan(0);
        expect(scroll).toBeLessThan(800);
        expect(minimapPointerToScrollTop(0, 100, 200, 1000, 10, 18)).toBe(0);
        expect(minimapPointerToScrollTop(100, 100, 200, 1000, 10, 18)).toBe(800);

        expect(clampMinimapScrollTop(900, 180, 900)).toBe(720);
        expect(clampMinimapScrollTop(-20, 180, 900)).toBe(0);
        expect(clampMinimapScrollTop(Number.NaN, 180, 900)).toBe(0);

        expect(minimapWheelDeltaPixels(3, 0, 200)).toBe(3);
        expect(minimapWheelDeltaPixels(3, 1, 200)).toBe(48);
        expect(minimapWheelDeltaPixels(1, 2, 200)).toBe(200);

        const thumb = { top: 10, height: 20, maxScroll: 100, travel: 80 };
        expect(minimapThumbsEqual(thumb, { ...thumb })).toBe(true);
        expect(minimapThumbsEqual(thumb, { ...thumb, top: 11 })).toBe(false);
    });

    it('swaps the observed scroll-content node only when the first child changes', () => {
        const a = { id: 'a' } as unknown as Element;
        const b = { id: 'b' } as unknown as Element;
        expect(nextObservedScrollContent(null, a)).toEqual({ unobserve: null, observe: a, next: a });
        expect(nextObservedScrollContent(a, a)).toEqual({ unobserve: null, observe: null, next: a });
        expect(nextObservedScrollContent(a, b)).toEqual({ unobserve: a, observe: b, next: b });
        expect(nextObservedScrollContent(a, null)).toEqual({ unobserve: a, observe: null, next: null });
    });

    it('maps an active find line to a marker ratio', () => {
        expect(minimapMarkerRatio(-1, 10)).toBeNull();
        expect(minimapMarkerRatio(0, 0)).toBeNull();
        expect(minimapMarkerRatio(0, 10)).toBeCloseTo(0.05);
        expect(minimapMarkerRatio(9, 10)).toBeCloseTo(0.95);
        expect(minimapMarkerRatio(10, 10)).toBeNull();

        const diff = [
            { type: 'unchanged' as const, content: 'a', newLineNum: 1 },
            { type: 'delete' as const, content: 'x' },
            { type: 'add' as const, content: 'b', newLineNum: 2 },
        ];
        expect(minimapMarkerRatio(1, 2, diff)).toBeCloseTo(2.5 / 3);
        expect(minimapMarkerRatio(4, 2, diff)).toBeNull();
    });

    it('chooses text vs bar rendering from row height', () => {
        expect(shouldRenderMinimapText(10, 40)).toBe(true);
        expect(shouldRenderMinimapText(100, 40)).toBe(false);
        expect(shouldRenderMinimapText(0, 40)).toBe(false);
        expect(colorForMinimapKind('add', colors)).toBe(colors.add);
        expect(colorForMinimapKind('plain', colors)).toBe(colors.text);
    });

    it('draws scaled text for short files and density bars for long files', () => {
        const fillText: string[] = [];
        const fillRect: number[][] = [];
        const ctx = {
            globalAlpha: 1,
            fillStyle: '',
            font: '',
            textBaseline: 'alphabetic' as CanvasTextBaseline,
            clearRect() {},
            fillRect(x: number, y: number, w: number, h: number) { fillRect.push([x, y, w, h]); },
            fillText(text: string) { fillText.push(text); },
            save() {},
            restore() {},
        };

        const shortLines = ['alpha', '  beta', ''];
        drawCodePreviewMinimap(ctx, {
            width: 64,
            height: 30,
            samples: buildMinimapSamples({ lines: shortLines }),
            lines: shortLines,
            colors,
        });
        expect(fillText).toEqual(['alpha', '  beta']);
        expect(fillRect).toEqual([]);

        fillText.length = 0;
        const longLines = Array.from({ length: 80 }, (_, i) => `line ${i} ${'x'.repeat(20)}`);
        drawCodePreviewMinimap(ctx, {
            width: 64,
            height: 40,
            samples: buildMinimapSamples({ lines: longLines }),
            lines: longLines,
            colors,
        });
        expect(fillText).toEqual([]);
        expect(fillRect.length).toBeGreaterThan(0);

        fillText.length = 0;
        fillRect.length = 0;
        const bucketed = downsampleMinimapSamples(buildMinimapSamples({ lines: longLines }), 10);
        drawCodePreviewMinimap(ctx, {
            width: 64,
            height: 40,
            samples: bucketed,
            lines: longLines,
            colors,
        });
        expect(fillText).toEqual([]);
        expect(fillRect.length).toBeGreaterThan(0);
    });
});
