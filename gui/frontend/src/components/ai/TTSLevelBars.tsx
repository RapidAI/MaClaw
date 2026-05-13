import { memo } from "react";

function TTSLevelBarsInner({ level, accentColor }: { level: number; accentColor: string }) {
    // 3 bars with staggered heights based on audio level.
    // Quantize to reduce re-renders from tiny fluctuations (step = 0.05).
    const q = Math.round(Math.min(1, Math.max(0, level)) * 20) / 20;
    const barHeights = [
        Math.max(3, q * 9),       // left bar
        Math.max(3, q * 12),      // center bar — tallest
        Math.max(3, q * 7),       // right bar
    ];
    return (
        <span aria-hidden="true" style={{ position: "absolute", bottom: 2, right: 1, display: "flex", alignItems: "flex-end", gap: "1px", height: "12px", pointerEvents: "none" }}>
            {barHeights.map((h, i) => (
                <span key={i} style={{ width: "2px", height: `${h}px`, borderRadius: "1px", background: accentColor, opacity: 0.9, transition: "height 80ms ease-out" }} />
            ))}
        </span>
    );
}

export const TTSLevelBars = memo(TTSLevelBarsInner);
