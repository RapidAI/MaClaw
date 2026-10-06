import { useLayoutEffect, useMemo, useRef, useState } from 'react';
import {
    buildTokenTrend,
    earliestRecordedDay,
    localISODate,
    smoothCurvePath,
    splitTrendSegments,
    TOKEN_USAGE_DAY_RETENTION,
    type TokenTrendBar,
    type TokenTrendGrain,
    type TokenUsageDayPoint,
} from './tokenUsageSeries';

type Props = {
    lang: string;
    provider: string;
    points: TokenUsageDayPoint[];
    aliases: Record<string, string[]>;
    formatTokens: (value: number) => string;
};

const grains: TokenTrendGrain[] = ['day', 'week', 'month'];

function textFor(lang: string, en: string, zhHans: string, zhHant: string) {
    if (lang === 'zh-Hans') return zhHans;
    if (lang === 'zh-Hant') return zhHant;
    return en;
}

export function TokenUsageTrend({ lang, provider, points, aliases, formatTokens }: Props) {
    const [grain, setGrain] = useState<TokenTrendGrain>('day');
    const today = localISODate();
    const series = useMemo(
        () => buildTokenTrend(points, provider, grain, today, aliases),
        [points, provider, grain, today, aliases],
    );
    const sum = series.reduce((total, point) => total + point.total, 0);
    const chartRef = useRef<SVGSVGElement>(null);
    const [width, setWidth] = useState(320);
    useLayoutEffect(() => {
        const chart = chartRef.current;
        if (!chart || typeof ResizeObserver === 'undefined') return;
        const measure = () => {
            const next = Math.round(chart.clientWidth);
            if (next < 2) return;
            setWidth((current) => (current === next ? current : next));
        };
        measure();
        const observer = new ResizeObserver(measure);
        observer.observe(chart);
        return () => observer.disconnect();
    }, []);
    const curve = useMemo(() => {
        const height = 96;
        const plotTop = 8;
        const plotBottom = height - 4;
        const max = Math.max(...series.map((point) => point.total), 1);
        const pad = 4;
        const xOf = (index: number) => {
            if (series.length <= 1) return width / 2;
            return pad + (index / (series.length - 1)) * Math.max(0, width - pad * 2);
        };
        const yOf = (total: number) => plotBottom - (total / max) * (plotBottom - plotTop);
        const pointAt = (index: number) => ({ point: series[index], x: xOf(index), y: yOf(series[index].total) });
        const segments = splitTrendSegments(series);
        // Draw one curve per contiguous recorded run. Joining runs with a single
        // path would draw usage through days that have no record at all, which
        // is exactly the confusion the recorded flag exists to prevent.
        const runs = segments
            .filter((segment) => segment.recorded)
            .map((segment) => {
                const points: Array<{ x: number; y: number }> = [];
                for (let index = segment.from; index <= segment.to; index += 1) {
                    const entry = pointAt(index);
                    points.push({ x: entry.x, y: entry.y });
                }
                const path = smoothCurvePath(points);
                const firstX = points[0]?.x ?? xOf(0);
                const lastX = points[points.length - 1]?.x ?? xOf(Math.max(0, series.length - 1));
                return {
                    path,
                    area: path && points.length > 1 ? `${path} L ${lastX} ${plotBottom} L ${firstX} ${plotBottom} Z` : '',
                };
            })
            .filter((run) => run.path);
        const gaps = segments
            .filter((segment) => !segment.recorded)
            .map((segment) => {
                const x1 = xOf(segment.from);
                const x2 = xOf(segment.to);
                return { x1, x2: x2 > x1 ? x2 : x1 + 1 };
            });
        // Per-point markers only help on sparse series. A fully recorded 30-day
        // view would otherwise render 30 dots and read as visual noise, so keep
        // the original density guard.
        const recordedPoints = segments
            .filter((segment) => segment.recorded)
            .flatMap((segment) => {
                const collected: Array<{ point: (typeof series)[number]; x: number; y: number }> = [];
                for (let index = segment.from; index <= segment.to; index += 1) collected.push(pointAt(index));
                return collected;
            });
        const showDots = series.length <= 12 || recordedPoints.length <= 12;
        return { height, plotBottom, pad, xOf, yOf, runs, gaps, recordedPoints, showDots };
    }, [series, width]);
    const rangeLabel = grain === 'day'
        ? textFor(lang, 'Last 30 days', '近30日', '近30日')
        : grain === 'week'
            ? textFor(lang, 'Last 8 weeks', '近8周', '近8週')
            : textFor(lang, `Last ${series.length} months`, `近${series.length}个月`, `近${series.length}個月`);
    const chartLabel = textFor(
        lang,
        `${rangeLabel} token trend, ${formatTokens(sum)} total`,
        `${rangeLabel} Token 趋势，合计 ${formatTokens(sum)}`,
        `${rangeLabel} Token 趨勢，合計 ${formatTokens(sum)}`,
    );
    // Name the first day the backend actually stored, so an empty stretch is
    // explained instead of looking like a rendering failure.
    const firstRecorded = earliestRecordedDay(series);
    const coverage = firstRecorded && firstRecorded > (series[0]?.key ?? '')
        ? {
            zhHans: `按日记录自 ${firstRecorded} 起（保留近 ${TOKEN_USAGE_DAY_RETENTION} 天，虚线段无记录）。更早的用量仍在总计里。`,
            zhHant: `按日記錄自 ${firstRecorded} 起（保留近 ${TOKEN_USAGE_DAY_RETENTION} 天，虛線段無記錄）。更早的用量仍在總計裡。`,
            en: `Daily records start ${firstRecorded}, kept for ${TOKEN_USAGE_DAY_RETENTION} days; dashed stretches have no record. Earlier usage stays in the total.`,
        }
        : {
            zhHans: `趋势只含按日记录，保留近 ${TOKEN_USAGE_DAY_RETENTION} 天，虚线段无记录。更早的用量仍在总计里。`,
            zhHant: `趨勢只含按日記錄，保留近 ${TOKEN_USAGE_DAY_RETENTION} 天，虛線段無記錄。更早的用量仍在總計裡。`,
            en: `Daily records kept for ${TOKEN_USAGE_DAY_RETENTION} days; dashed stretches have no record. Earlier usage stays in the total.`,
        };

    return (
        <section className="token-usage-trend" data-testid="token-usage-trend" data-grain={grain} data-total={sum}>
            <div className="token-usage-trend__toolbar">
                <div className="token-usage-trend__grains" role="group" aria-label={textFor(lang, 'Usage trend range', '用量趋势范围', '用量趨勢範圍')}>
                    {grains.map((item) => {
                        const label = item === 'day'
                            ? textFor(lang, 'Day', '日', '日')
                            : item === 'week'
                                ? textFor(lang, 'Week', '周', '週')
                                : textFor(lang, 'Month', '月', '月');
                        return (
                            <button
                                key={item}
                                type="button"
                                className={`token-usage-trend__grain${grain === item ? ' is-active' : ''}`}
                                aria-pressed={grain === item}
                                onClick={() => setGrain(item)}
                            >
                                {label}
                            </button>
                        );
                    })}
                </div>
                <span className="token-usage-trend__sum">{rangeLabel} {formatTokens(sum)}</span>
            </div>
            <svg ref={chartRef} className="token-usage-trend__chart" viewBox={`0 0 ${width} ${curve.height}`} role="img" aria-label={chartLabel}>
                <line className="token-usage-trend__axis" x1="0" y1={curve.plotBottom} x2={width} y2={curve.plotBottom} />
                {curve.gaps.map((gap) => (
                    <line
                        key={`gap-${gap.x1}-${gap.x2}`}
                        className="token-usage-trend__gap"
                        x1={gap.x1}
                        y1={curve.plotBottom}
                        x2={gap.x2}
                        y2={curve.plotBottom}
                    />
                ))}
                {sum > 0 && curve.runs.map((run, index) => (
                    <g key={`run-${index}`}>
                        {run.area && <path className="token-usage-trend__area" d={run.area} />}
                        <path className="token-usage-trend__line" d={run.path} />
                    </g>
                ))}
                {curve.recordedPoints.map((entry) => (
                    entry.point.total > 0 && curve.showDots && (
                        <circle
                            key={`dot-${entry.point.key}`}
                            className="token-usage-trend__dot"
                            cx={entry.x}
                            cy={entry.y}
                            r="2.5"
                        />
                    )
                ))}
                {series.map((point, index) => {
                    const step = series.length <= 1 ? width : Math.max(0, width - curve.pad * 2) / (series.length - 1);
                    const center = curve.xOf(index);
                    const left = index === 0 ? 0 : center - step / 2;
                    const right = index === series.length - 1 ? width : center + step / 2;
                    const tip = !point.recorded
                        ? `${point.detail} · ${textFor(lang, 'no record', '无记录', '無記錄')}`
                        : point.input > 0 || point.output > 0
                            ? `${point.detail} · ${formatTokens(point.total)} (${textFor(lang, 'in', '输入', '輸入')} ${formatTokens(point.input)} / ${textFor(lang, 'out', '输出', '輸出')} ${formatTokens(point.output)})`
                            : `${point.detail} · ${formatTokens(point.total)}`;
                    return (
                        <g key={point.key} data-trend-key={point.key} data-recorded={point.recorded ? 'true' : 'false'}>
                            <rect className="token-usage-trend__hit" x={left} y={0} width={Math.max(0, right - left)} height={curve.height} fill="transparent">
                                <title>{tip}</title>
                            </rect>
                        </g>
                    );
                })}
            </svg>
            <div className="token-usage-trend__labels" aria-hidden="true">
                <span className="is-start" style={{ left: `${(curve.xOf(0) / width) * 100}%` }}>{series[0]?.axisLabel}</span>
                {series.length > 2 && (
                    <span className="is-mid" style={{ left: `${(curve.xOf(Math.floor((series.length - 1) / 2)) / width) * 100}%` }}>
                        {series[Math.floor((series.length - 1) / 2)]?.axisLabel}
                    </span>
                )}
                {series.length > 1 && (
                    <span className="is-end" style={{ right: `${((width - curve.xOf(series.length - 1)) / width) * 100}%` }}>
                        {series[series.length - 1]?.axisLabel}
                    </span>
                )}
            </div>
            <p className="token-usage-trend__note">
                {textFor(lang, coverage.en, coverage.zhHans, coverage.zhHant)}
            </p>
        </section>
    );
}
