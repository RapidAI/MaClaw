import { memo, useEffect, useRef } from "react";

/**
 * Audio level visualization bars for the TTS button.
 *
 * PERFORMANCE: This component does NOT re-render on level changes.
 * It subscribes to a shared "tts-level" custom event and updates bar heights
 * via direct DOM manipulation (ref.style.height), completely bypassing React's
 * reconciliation. The parent only re-renders when ttsPlaying toggles.
 */
function TTSLevelBarsInner({ accentColor }: { accentColor: string }) {
    const barsRef = useRef<(HTMLSpanElement | null)[]>([]);
    const containerRef = useRef<HTMLSpanElement>(null);

    useEffect(() => {
        const bars = barsRef.current;
        // Multipliers for each bar to create staggered heights
        const multipliers = [0.7, 1.0, 0.8, 0.55];
        const minHeights = [5, 5, 5, 5];

        let lastQ = -1;
        let raf = 0;

        const onLevel = (e: Event) => {
            const level = (e as CustomEvent<number>).detail;
            const q = Math.round(Math.min(1, Math.max(0, level)) * 20) / 20;
            if (q === lastQ) return;
            lastQ = q;

            if (raf) cancelAnimationFrame(raf);
            raf = requestAnimationFrame(() => {
                for (let i = 0; i < bars.length; i++) {
                    const bar = bars[i];
                    if (!bar) continue;
                    const h = Math.max(minHeights[i], q * 20 * multipliers[i]);
                    bar.style.height = `${h}px`;
                }
                // Toggle idle animation class based on signal presence
                const container = containerRef.current;
                if (container) {
                    container.classList.toggle("tts-bars-idle", q <= 0.02);
                }
            });
        };

        window.addEventListener("tts-level", onLevel);
        return () => {
            window.removeEventListener("tts-level", onLevel);
            if (raf) cancelAnimationFrame(raf);
        };
    }, []);

    return (
        <span
            ref={containerRef}
            aria-hidden="true"
            className="tts-bars-idle"
            style={{
                position: "absolute",
                inset: 0,
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
                gap: "2px",
                pointerEvents: "none",
            }}
        >
            {[0, 1, 2, 3].map((i) => (
                <span
                    key={i}
                    ref={(el) => { barsRef.current[i] = el; }}
                    style={{
                        width: "3px",
                        height: "5px",
                        borderRadius: "1.5px",
                        background: accentColor,
                        opacity: 0.9,
                        transition: "height 60ms ease-out",
                    }}
                />
            ))}
        </span>
    );
}

export const TTSLevelBars = memo(TTSLevelBarsInner);
