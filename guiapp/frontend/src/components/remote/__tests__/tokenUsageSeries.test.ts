import { describe, expect, it } from 'vitest';
import {
    buildTokenTrend,
    earliestRecordedDay,
    shiftISODate,
    smoothCurvePath,
    splitTrendSegments,
} from '../tokenUsageSeries';

const aliases = {
    '智谱': ['智谱龙芯', 'GLM(智谱)', 'GLM (智谱)'],
};

describe('buildTokenTrend', () => {
    const today = '2026-10-05';
    const points = [
        { date: today, provider: 'MiniMax', input_tokens: 10, output_tokens: 2, total_tokens: 12 },
        { date: '2026-10-04', provider: 'MiniMax', input_tokens: 5, output_tokens: 0, total_tokens: 5 },
        { date: '2026-09-30', provider: 'MiniMax', input_tokens: 7, output_tokens: 0, total_tokens: 7 },
        { date: '2026-10-05', provider: 'Other', input_tokens: 100, output_tokens: 0, total_tokens: 100 },
        { date: '2026-10-03', provider: 'GLM (智谱)', input_tokens: 4, output_tokens: 1, total_tokens: 5 },
    ];

    it('fills the last 30 days and keeps other providers out', () => {
        const bars = buildTokenTrend(points, 'MiniMax', 'day', today);
        expect(bars).toHaveLength(30);
        expect(bars[bars.length - 1]).toMatchObject({ key: today, total: 12, input: 10, output: 2 });
        expect(bars.find((bar) => bar.key === '2026-10-04')?.total).toBe(5);
        expect(bars.find((bar) => bar.key === '2026-09-30')?.total).toBe(7);
        expect(bars.reduce((sum, bar) => sum + bar.total, 0)).toBe(24);
    });

    it('groups weeks from Monday and months by calendar month', () => {
        const weeks = buildTokenTrend(points, 'MiniMax', 'week', today);
        expect(weeks).toHaveLength(8);
        expect(weeks[weeks.length - 1]).toMatchObject({ key: '2026-10-05', total: 12, detail: '2026-10-05' });
        expect(weeks[weeks.length - 2]).toMatchObject({ key: '2026-09-28', total: 12, detail: '2026-09-28 – 2026-10-04' });

        const months = buildTokenTrend(points, 'MiniMax', 'month', today);
        expect(months.map((bar) => bar.key)).toEqual(['2026-08', '2026-09', '2026-10']);
        expect(months.find((bar) => bar.key === '2026-10')?.total).toBe(17);
        expect(months.find((bar) => bar.key === '2026-09')?.total).toBe(7);
    });

    it('drops days outside the retained window and invalid calendar dates', () => {
        const bars = buildTokenTrend([
            { date: '2026-08-03', provider: 'MiniMax', total_tokens: 50 },
            { date: '2026-08-04', provider: 'MiniMax', total_tokens: 3 },
            { date: '2026-02-31', provider: 'MiniMax', total_tokens: 9 },
            { date: '2026-10-06', provider: 'MiniMax', total_tokens: 8 },
        ], 'MiniMax', 'month', '2026-10-05');
        expect(bars.find((bar) => bar.key === '2026-08')?.total).toBe(3);
        expect(bars.reduce((sum, bar) => sum + bar.total, 0)).toBe(3);
    });

    it('follows provider aliases', () => {
        const bars = buildTokenTrend(points, '智谱', 'day', today, aliases);
        expect(bars.find((bar) => bar.key === '2026-10-03')?.total).toBe(5);
        expect(bars.reduce((sum, bar) => sum + bar.total, 0)).toBe(5);
    });

    it('adds same-day usage stored under an older alias name', () => {
        const bars = buildTokenTrend([
            { date: '2026-10-05', provider: 'GLM (智谱)', total_tokens: 10 },
            { date: '2026-10-05', provider: '智谱', total_tokens: 100 },
            { date: '2026-10-04', provider: '智谱', total_tokens: 7 },
        ], 'GLM (智谱)', 'day', today, {
            'GLM (智谱)': ['智谱', 'GLM(智谱)'],
        });
        expect(bars.find((bar) => bar.key === '2026-10-05')?.total).toBe(110);
        expect(bars.find((bar) => bar.key === '2026-10-04')?.total).toBe(7);
    });
});

describe('recorded-day semantics', () => {
    const today = '2026-10-06';

    it('marks days without a backend bucket as unrecorded, not as zero usage', () => {
        const bars = buildTokenTrend([
            { date: today, provider: 'MiniMax', input_tokens: 90, output_tokens: 10, total_tokens: 100 },
        ], 'MiniMax', 'day', today);

        const filled = bars.filter((bar) => !bar.recorded);
        // Only the day the backend actually stored may claim to be recorded.
        expect(filled.every((bar) => bar.total === 0)).toBe(true);
        expect(bars.filter((bar) => bar.recorded).map((bar) => bar.key)).toEqual([today]);
        expect(earliestRecordedDay(bars)).toBe(today);
    });

    it('reports no earliest day when nothing was ever recorded', () => {
        const bars = buildTokenTrend([], 'MiniMax', 'day', today);
        expect(bars.every((bar) => bar.recorded === false)).toBe(true);
        expect(earliestRecordedDay(bars)).toBe('');
    });

    it('keeps a recorded day with tokens distinct from an absent day', () => {
        const bars = buildTokenTrend([
            { date: '2026-10-04', provider: 'MiniMax', input_tokens: 5, output_tokens: 5, total_tokens: 10 },
        ], 'MiniMax', 'day', today);
        const recorded = bars.find((bar) => bar.key === '2026-10-04');
        const absent = bars.find((bar) => bar.key === '2026-10-05');
        expect(recorded).toMatchObject({ recorded: true, total: 10 });
        expect(absent).toMatchObject({ recorded: false, total: 0 });
    });

    it('propagates recorded through week and month rollups', () => {
        const points = [{ date: '2026-10-05', provider: 'MiniMax', input_tokens: 4, output_tokens: 1, total_tokens: 5 }];
        const weeks = buildTokenTrend(points, 'MiniMax', 'week', '2026-10-07');
        expect(weeks.filter((bar) => !bar.recorded).every((bar) => bar.total === 0)).toBe(true);
        expect(weeks.some((bar) => bar.recorded && bar.total === 5)).toBe(true);

        const months = buildTokenTrend(points, 'MiniMax', 'month', today);
        expect(months.find((bar) => bar.key === '2026-10')).toMatchObject({ recorded: true, total: 5 });
        expect(months.find((bar) => bar.key === '2026-09')).toMatchObject({ recorded: false, total: 0 });
    });

    it('splits runs so a leading history gap is drawn separately', () => {
        const bars = buildTokenTrend([
            { date: today, provider: 'MiniMax', input_tokens: 9, output_tokens: 1, total_tokens: 10 },
        ], 'MiniMax', 'day', today);
        const segments = splitTrendSegments(bars);
        expect(segments).toHaveLength(2);
        expect(segments[0]).toMatchObject({ from: 0, to: 28, recorded: false });
        expect(segments[1]).toMatchObject({ from: 29, to: 29, recorded: true });
    });

    it('collapses to one segment when the whole window is recorded', () => {
        const points = Array.from({ length: 30 }, (_, offset) => ({
            date: shiftISODate(today, -(29 - offset)),
            provider: 'MiniMax',
            total_tokens: 5,
        }));
        const segments = splitTrendSegments(buildTokenTrend(points, 'MiniMax', 'day', today));
        expect(segments).toHaveLength(1);
        expect(segments[0]).toMatchObject({ from: 0, to: 29, recorded: true });
    });

    it('breaks a contiguous run when a middle day is missing', () => {
        const bars = buildTokenTrend([
            { date: '2026-10-03', provider: 'MiniMax', total_tokens: 5 },
            { date: '2026-10-06', provider: 'MiniMax', total_tokens: 5 },
        ], 'MiniMax', 'day', today);
        const segments = splitTrendSegments(bars);
        expect(segments.map((segment) => segment.recorded)).toEqual([false, true, false, true]);
    });
});

describe('smoothCurvePath', () => {
    function sampleYs(path: string): number[] {
        const numbers = path.match(/-?\d+(?:\.\d+)?/g)?.map(Number) ?? [];
        const ys: number[] = [];
        if (numbers.length >= 2) ys.push(numbers[1]);
        for (let index = 2; index + 5 < numbers.length; index += 6) {
            const y0 = ys[ys.length - 1];
            const c1 = numbers[index + 1];
            const c2 = numbers[index + 3];
            const y1 = numbers[index + 5];
            for (let step = 1; step <= 8; step += 1) {
                const t = step / 8;
                const u = 1 - t;
                ys.push((u ** 3) * y0 + 3 * (u ** 2) * t * c1 + 3 * u * (t ** 2) * c2 + (t ** 3) * y1);
            }
        }
        return ys;
    }

    it('passes through the points and does not overshoot a spike', () => {
        const path = smoothCurvePath([
            { x: 0, y: 90 },
            { x: 40, y: 10 },
            { x: 80, y: 90 },
        ]);
        expect(path.startsWith('M 0 90')).toBe(true);
        expect(path.endsWith('80 90')).toBe(true);
        const samples = sampleYs(path);
        expect(Math.min(...samples)).toBeGreaterThanOrEqual(10 - 0.05);
        expect(Math.max(...samples)).toBeLessThanOrEqual(90 + 0.05);
    });
});
