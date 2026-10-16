export type TokenTrendGrain = 'day' | 'week' | 'month';

export type TokenUsageDayPoint = {
    date?: string;
    Date?: string;
    provider?: string;
    Provider?: string;
    input_tokens?: number;
    output_tokens?: number;
    total_tokens?: number;
    InputTokens?: number;
    OutputTokens?: number;
    TotalTokens?: number;
};

export type TokenTrendBar = {
    key: string;
    axisLabel: string;
    detail: string;
    input: number;
    output: number;
    total: number;
    /**
     * False when the backend reported no bucket for this day at all. The day
     * store skips all-zero deltas, so a missing bar and a recorded zero are
     * different facts and must not render identically.
     */
    recorded: boolean;
};

export type TokenTrendSegment = {
    from: number;
    to: number;
    recorded: boolean;
};

export const TOKEN_TREND_DAY_SPAN = 30;
export const TOKEN_TREND_WEEK_SPAN = 8;
/** Inclusive local days kept by the backend day buckets. */
export const TOKEN_USAGE_DAY_RETENTION = 62;

export function localISODate(now = new Date()): string {
    const year = now.getFullYear();
    const month = String(now.getMonth() + 1).padStart(2, '0');
    const day = String(now.getDate()).padStart(2, '0');
    return `${year}-${month}-${day}`;
}

function parseISODate(iso: string): { year: number; month: number; day: number } | null {
    const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(iso);
    if (!match) return null;
    const year = Number(match[1]);
    const month = Number(match[2]);
    const day = Number(match[3]);
    if (month < 1 || month > 12 || day < 1 || day > 31) return null;
    const date = new Date(Date.UTC(year, month - 1, day));
    if (date.getUTCFullYear() !== year || date.getUTCMonth() !== month - 1 || date.getUTCDate() !== day) return null;
    return { year, month, day };
}

export function shiftISODate(iso: string, days: number): string {
    const parsed = parseISODate(iso);
    if (!parsed) return iso;
    const date = new Date(Date.UTC(parsed.year, parsed.month - 1, parsed.day));
    date.setUTCDate(date.getUTCDate() + days);
    return date.toISOString().slice(0, 10);
}

function weekStart(iso: string): string {
    const parsed = parseISODate(iso);
    if (!parsed) return iso;
    const weekday = new Date(Date.UTC(parsed.year, parsed.month - 1, parsed.day)).getUTCDay();
    const mondayOffset = weekday === 0 ? 6 : weekday - 1;
    return shiftISODate(iso, -mondayOffset);
}

function shiftMonth(yearMonth: string, delta: number): string {
    const [yearText, monthText] = yearMonth.split('-');
    const date = new Date(Date.UTC(Number(yearText), Number(monthText) - 1 + delta, 1));
    const month = String(date.getUTCMonth() + 1).padStart(2, '0');
    return `${date.getUTCFullYear()}-${month}`;
}

function axisDay(iso: string): string {
    const parsed = parseISODate(iso);
    if (!parsed) return iso;
    return `${parsed.month}/${parsed.day}`;
}

function finiteTokens(value: unknown): number {
    const parsed = Number(value ?? 0);
    return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
}

function normalizeDayPoint(point: TokenUsageDayPoint) {
    const date = String(point.date ?? point.Date ?? '');
    const provider = String(point.provider ?? point.Provider ?? '');
    const input = finiteTokens(point.input_tokens ?? point.InputTokens);
    const output = finiteTokens(point.output_tokens ?? point.OutputTokens);
    const reported = finiteTokens(point.total_tokens ?? point.TotalTokens);
    const split = input + output;
    return { date, provider, input, output, total: split || reported };
}

export function buildTokenTrend(
    points: TokenUsageDayPoint[] | null | undefined,
    provider: string,
    grain: TokenTrendGrain,
    today: string,
    aliases: Record<string, string[]> = {},
): TokenTrendBar[] {
    const accepted = new Set([provider, ...(aliases[provider] || [])].filter(Boolean));
    const earliest = shiftISODate(today, -TOKEN_USAGE_DAY_RETENTION);
    const byDate = new Map<string, { input: number; output: number; total: number; recorded: boolean }>();
    for (const raw of points || []) {
        const point = normalizeDayPoint(raw);
        if (!accepted.has(point.provider) || !parseISODate(point.date)) continue;
        if (point.date < earliest || point.date > today) continue;
        const prev = byDate.get(point.date) || { input: 0, output: 0, total: 0, recorded: false };
        prev.input += point.input;
        prev.output += point.output;
        prev.total += point.total;
        prev.recorded = true;
        byDate.set(point.date, prev);
    }

    if (grain === 'day') {
        const bars: TokenTrendBar[] = [];
        for (let offset = TOKEN_TREND_DAY_SPAN - 1; offset >= 0; offset -= 1) {
            const date = shiftISODate(today, -offset);
            const hit = byDate.get(date);
            bars.push({
                key: date,
                axisLabel: axisDay(date),
                detail: date,
                input: hit?.input ?? 0,
                output: hit?.output ?? 0,
                total: hit?.total ?? 0,
                recorded: !!hit,
            });
        }
        return bars;
    }

    if (grain === 'week') {
        const currentWeek = weekStart(today);
        const bars: TokenTrendBar[] = [];
        for (let offset = TOKEN_TREND_WEEK_SPAN - 1; offset >= 0; offset -= 1) {
            const start = shiftISODate(currentWeek, -7 * offset);
            const end = shiftISODate(start, 6);
            const visibleEnd = end > today ? today : end;
            let input = 0;
            let output = 0;
            let total = 0;
            let recorded = false;
            for (let day = 0; day < 7; day += 1) {
                const hit = byDate.get(shiftISODate(start, day));
                if (!hit) continue;
                input += hit.input;
                output += hit.output;
                total += hit.total;
                recorded = true;
            }
            bars.push({
                key: start,
                axisLabel: axisDay(start),
                detail: start === visibleEnd ? start : `${start} – ${visibleEnd}`,
                input,
                output,
                total,
                recorded,
            });
        }
        return bars;
    }

    const firstMonth = earliest.slice(0, 7);
    const currentMonth = today.slice(0, 7);
    const bars: TokenTrendBar[] = [];
    for (let key = firstMonth; key <= currentMonth; key = shiftMonth(key, 1)) {
        let input = 0;
        let output = 0;
        let total = 0;
        let recorded = false;
        for (const [date, hit] of byDate) {
            if (!date.startsWith(`${key}-`)) continue;
            input += hit.input;
            output += hit.output;
            total += hit.total;
            recorded = true;
        }
        const monthNumber = Number(key.slice(5, 7));
        bars.push({
            key,
            axisLabel: `${key.slice(0, 4)}/${monthNumber}`,
            detail: key,
            input,
            output,
            total,
            recorded,
        });
        if (bars.length > 6) break;
    }
    return bars;
}

/**
 * Splits a series into maximal runs of recorded / unrecorded buckets so the
 * chart can draw a dashed "no data" stretch instead of a flat zero line.
 */
export function splitTrendSegments(series: TokenTrendBar[]): TokenTrendSegment[] {
    if (series.length === 0) return [];
    const segments: TokenTrendSegment[] = [];
    let start = 0;
    for (let index = 1; index <= series.length; index += 1) {
        const atEnd = index === series.length;
        if (!atEnd && series[index].recorded === series[start].recorded) continue;
        segments.push({ from: start, to: atEnd ? series.length - 1 : index - 1, recorded: series[start].recorded });
        start = index;
    }
    return segments;
}

/** Earliest day the backend actually recorded, or '' when nothing was stored. */
export function earliestRecordedDay(series: TokenTrendBar[]): string {
    return series.find((bar) => bar.recorded)?.key ?? '';
}

function roundCoord(value: number): number {
    return Math.round(value * 100) / 100;
}

/** Monotone cubic tangents. Each segment stays between its two values, so a spike cannot overshoot. */
function monotoneTangents(xs: number[], ys: number[]): number[] {
    const count = xs.length;
    const slopes: number[] = [];
    const tangents = new Array<number>(count).fill(0);
    if (count < 2) return tangents;
    for (let index = 0; index < count - 1; index += 1) {
        const dx = xs[index + 1] - xs[index];
        slopes.push(dx === 0 ? 0 : (ys[index + 1] - ys[index]) / dx);
    }
    tangents[0] = slopes[0];
    tangents[count - 1] = slopes[count - 2];
    for (let index = 1; index < count - 1; index += 1) {
        tangents[index] = slopes[index - 1] * slopes[index] <= 0 ? 0 : (slopes[index - 1] + slopes[index]) / 2;
    }
    for (let index = 0; index < count - 1; index += 1) {
        if (slopes[index] === 0) {
            tangents[index] = 0;
            tangents[index + 1] = 0;
            continue;
        }
        const a = tangents[index] / slopes[index];
        const b = tangents[index + 1] / slopes[index];
        const scale = Math.hypot(a, b);
        if (scale > 3) {
            const limit = 3 / scale;
            tangents[index] = limit * a * slopes[index];
            tangents[index + 1] = limit * b * slopes[index];
        }
    }
    return tangents;
}

/** Smooth curve through the points. One point is a move; two or more are monotone cubics. */
export function smoothCurvePath(points: Array<{ x: number; y: number }>): string {
    if (points.length === 0) return '';
    const xs = points.map((point) => point.x);
    const ys = points.map((point) => point.y);
    let path = `M ${roundCoord(xs[0])} ${roundCoord(ys[0])}`;
    if (points.length === 1) return path;
    const tangents = monotoneTangents(xs, ys);
    for (let index = 0; index < points.length - 1; index += 1) {
        const dx = xs[index + 1] - xs[index];
        const c1x = roundCoord(xs[index] + dx / 3);
        const c1y = roundCoord(ys[index] + tangents[index] * dx / 3);
        const c2x = roundCoord(xs[index + 1] - dx / 3);
        const c2y = roundCoord(ys[index + 1] - tangents[index + 1] * dx / 3);
        path += ` C ${c1x} ${c1y} ${c2x} ${c2y} ${roundCoord(xs[index + 1])} ${roundCoord(ys[index + 1])}`;
    }
    return path;
}
