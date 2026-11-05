import { colors } from "./styles";

// Pie chart color palette — visually distinct, works on both light and dark themes.
export const PIE_COLORS = [
    "#2f5f98",
    "#4f7f6f",
    "#64748b",
    "#183b63",
    "#5b7898",
    "#8aa4bf",
    "#526579",
    "#7f8da1",
    "#3f5872",
    "#6b8baa",
    "#789185",
    "#94a3b8",
    "#b7c5d4",
];

export function MemoryStatusStatCard({ label, value }: { label: string; value: number | string }) {
    return (
        <div className="memory-status-stat-card">
            <div className="memory-status-stat-card__value">{value}</div>
            <div className="memory-status-stat-card__label">{label}</div>
        </div>
    );
}

/** Pure SVG donut/pie chart — no external dependencies. */
export function PieChart({ data, size = 160, centerLabel, ariaLabel }: { data: Array<{ category: string; label: string; count: number; percent: number }>; size?: number; centerLabel: string; ariaLabel: string }) {
    const cx = size / 2;
    const cy = size / 2;
    const outerR = size / 2 - 4;
    const innerR = outerR * 0.55; // donut hole

    // Build arc paths.
    let startAngle = -90; // start from top
    const arcs: Array<{ path: string; color: string; label: string; pct: number }> = [];

    for (let i = 0; i < data.length; i++) {
        const pct = data[i].percent;
        const sweep = (pct / 100) * 360;
        if (sweep < 0.5) continue; // skip tiny slices

        const endAngle = startAngle + sweep;
        const largeArc = sweep > 180 ? 1 : 0;

        const toRad = (deg: number) => (deg * Math.PI) / 180;
        const x1o = cx + outerR * Math.cos(toRad(startAngle));
        const y1o = cy + outerR * Math.sin(toRad(startAngle));
        const x2o = cx + outerR * Math.cos(toRad(endAngle));
        const y2o = cy + outerR * Math.sin(toRad(endAngle));
        const x1i = cx + innerR * Math.cos(toRad(endAngle));
        const y1i = cy + innerR * Math.sin(toRad(endAngle));
        const x2i = cx + innerR * Math.cos(toRad(startAngle));
        const y2i = cy + innerR * Math.sin(toRad(startAngle));

        const path = [
            `M ${x1o} ${y1o}`,
            `A ${outerR} ${outerR} 0 ${largeArc} 1 ${x2o} ${y2o}`,
            `L ${x1i} ${y1i}`,
            `A ${innerR} ${innerR} 0 ${largeArc} 0 ${x2i} ${y2i}`,
            "Z",
        ].join(" ");

        arcs.push({ path, color: PIE_COLORS[i % PIE_COLORS.length], label: data[i].label, pct });
        startAngle = endAngle;
    }

    // Center text: total count.
    const total = data.reduce((s, d) => s + d.count, 0);

    return (
        <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} role="img" aria-label={ariaLabel}>
            {arcs.map((a, i) => (
                <path key={i} d={a.path} fill={a.color} stroke={colors.surface} strokeWidth={1.5}>
                    <title>{a.label}: {a.pct.toFixed(1)}%</title>
                </path>
            ))}
            {/* Center label */}
            <text x={cx} y={cy - 6} textAnchor="middle" fill="currentColor" fontSize={size * 0.13} fontWeight={700}>
                {total}
            </text>
            <text x={cx} y={cy + 10} textAnchor="middle" fill="currentColor" fontSize={size * 0.075} opacity={0.6}>
                {centerLabel}
            </text>
        </svg>
    );
}
