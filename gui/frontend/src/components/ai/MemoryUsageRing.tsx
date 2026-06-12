import { useEffect, useState, useRef } from "react";
import type { Theme } from "./aiAssistantPanelTheme";
import { getWailsAppModule } from "../../utils/wailsAppModule";

interface MemoryUsageRingProps {
    theme: Theme;
    themeMode: "light" | "dark";
    lang: string;
    size?: number;
}

interface MemoryStatus {
    total_entries: number;
    max_capacity: number;
    capacity_percent: number;
}

// Cached reference to the Wails binding — resolved once, reused thereafter.
let cachedGetMemoryStatus: (() => Promise<any>) | null = null;

async function resolveGetMemoryStatus(): Promise<(() => Promise<any>) | null> {
    if (cachedGetMemoryStatus) return cachedGetMemoryStatus;
    try {
        const mod = await getWailsAppModule();
        cachedGetMemoryStatus = mod.GetMemoryStatus;
        return cachedGetMemoryStatus;
    } catch {
        return null;
    }
}

/**
 * A tiny SVG ring chart showing memory usage percentage.
 * Self-contained: fetches data from the Wails binding on mount and every 60s.
 */
export function MemoryUsageRing({ theme: t, themeMode, lang, size = 22 }: MemoryUsageRingProps) {
    const [status, setStatus] = useState<MemoryStatus | null>(null);
    const mountedRef = useRef(true);

    useEffect(() => {
        mountedRef.current = true;

        let interval: ReturnType<typeof setInterval> | null = null;

        const fetchStatus = async (fn: () => Promise<any>) => {
            if (!mountedRef.current) return;
            try {
                const result = await fn();
                if (mountedRef.current && result && typeof result.capacity_percent === "number") {
                    setStatus(result as MemoryStatus);
                }
            } catch {
                // Silently ignore — ring stays hidden or shows last known state
            }
        };

        resolveGetMemoryStatus().then((fn) => {
            if (!fn || !mountedRef.current) return;
            fetchStatus(fn);
            interval = setInterval(() => fetchStatus(fn), 60_000);
        });

        return () => {
            mountedRef.current = false;
            if (interval) clearInterval(interval);
        };
    }, []);

    if (!status) return null;

    const percent = Math.min(100, Math.max(0, status.capacity_percent));
    const strokeWidth = 2.5;
    const radius = (size - strokeWidth) / 2;
    const circumference = 2 * Math.PI * radius;
    const dashOffset = circumference * (1 - percent / 100);

    // Color based on usage level
    const ringColor = percent >= 90
        ? (themeMode === "dark" ? "#e07a72" : "#c43d34")  // muted red — critical
        : percent >= 70
            ? (themeMode === "dark" ? "#c7d7e8" : "#64748b")  // neutral attention
            : (themeMode === "dark" ? "#7aa89a" : "#4f7f6f"); // muted green — healthy

    const trackColor = themeMode === "dark" ? "rgba(148, 163, 184, 0.18)" : "rgba(15, 23, 42, 0.08)";

    const tooltip = lang?.startsWith("zh")
        ? `记忆 ${status.total_entries}/${status.max_capacity} (${Math.round(percent)}%)`
        : `Memory ${status.total_entries}/${status.max_capacity} (${Math.round(percent)}%)`;

    return (
        <span
            title={tooltip}
            aria-label={tooltip}
            style={{
                display: "inline-flex",
                alignItems: "center",
                justifyContent: "center",
                width: `${size}px`,
                height: `${size}px`,
                flexShrink: 0,
                cursor: "default",
                position: "relative",
            }}
        >
            <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} style={{ transform: "rotate(-90deg)" }}>
                {/* Background track */}
                <circle
                    cx={size / 2}
                    cy={size / 2}
                    r={radius}
                    fill="none"
                    stroke={trackColor}
                    strokeWidth={strokeWidth}
                />
                {/* Usage arc */}
                <circle
                    cx={size / 2}
                    cy={size / 2}
                    r={radius}
                    fill="none"
                    stroke={ringColor}
                    strokeWidth={strokeWidth}
                    strokeDasharray={circumference}
                    strokeDashoffset={dashOffset}
                    strokeLinecap="round"
                />
            </svg>
            {/* Center percentage text */}
            <span style={{
                position: "absolute",
                fontSize: `${Math.max(7, size * 0.34)}px`,
                fontWeight: 600,
                color: t.textMuted,
                lineHeight: 1,
                userSelect: "none",
                pointerEvents: "none",
            }}>
                {Math.round(percent)}
            </span>
        </span>
    );
}
