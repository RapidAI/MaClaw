import { memo } from "react";

function TTSLevelBarsInner({ level, accentColor }: { level: number; accentColor: string }) {
    // 4 bars with staggered heights based on audio level, centered over the button icon.
    // Quantize to reduce re-renders from tiny fluctuations (step = 0.05).
    const q = Math.round(Math.min(1, Math.max(0, level)) * 20) / 20;
    const barHeights = [
        Math.max(4, q * 14),      // bar 1
        Math.max(4, q * 20),      // bar 2 — tallest
        Math.max(4, q * 16),      // bar 3
        Math.max(4, q * 11),      // bar 4
    ];
    return (
        <span aria-hidden="true" style={{ position: "absolute", inset: 0, display: "flex", alignItems: "center", justifyContent: "center", gap: "2px", pointerEvents: "none" }}>
            {barHeights.map((h, i) => (
                <span key={i} style={{ width: "3px", height: `${h}px`, borderRadius: "1.5px", background: accentColor, opacity: 0.85, transition: "height 60ms ease-out" }} />
            ))}
        </span>
    );
}

export const TTSLevelBars = memo(TTSLevelBarsInner);
