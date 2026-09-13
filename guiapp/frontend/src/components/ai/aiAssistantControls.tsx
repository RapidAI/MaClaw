import { useEffect, useRef } from "react";
import { localizeText } from "./aiAssistantI18n";
import { baseWindowControlBtnStyle, type Theme } from "./aiAssistantPanelTheme";

export const miniActionButtonStyle: React.CSSProperties = {
    flex: 1,
    minWidth: 0,
    border: "1px solid var(--theme-border, #cbd5e1)",
    borderRadius: "8px",
    background: "var(--theme-surface, #ffffff)",
    color: "var(--theme-text-primary, #334155)",
    fontSize: "11px",
    fontWeight: 600,
    padding: "5px 8px",
    cursor: "pointer",
    whiteSpace: "nowrap",
    overflow: "hidden",
    textOverflow: "ellipsis",
};

const NUM_BARS = 8;

export function VoiceLevelVisualizer({ onAudioLevelRef, isSpeaking, themeColor, speakingColor }: {
    onAudioLevelRef: React.MutableRefObject<((level: number) => void) | null>;
    isSpeaking: boolean;
    themeColor: string;
    speakingColor: string;
}) {
    const barsRef = useRef<HTMLDivElement | null>(null);
    const levelsRef = useRef(new Float32Array(NUM_BARS));
    const colorRef = useRef(themeColor);
    colorRef.current = isSpeaking ? speakingColor : themeColor;

    useEffect(() => {
        let frameId = 0;
        const levels = levelsRef.current;
        onAudioLevelRef.current = (level: number) => {
            for (let i = 0; i < NUM_BARS - 1; i++) levels[i] = levels[i + 1];
            levels[NUM_BARS - 1] = level;
            if (!frameId) {
                frameId = requestAnimationFrame(() => {
                    frameId = 0;
                    const container = barsRef.current;
                    if (!container) return;
                    const bars = container.children;
                    const color = colorRef.current;
                    for (let i = 0; i < bars.length && i < NUM_BARS; i++) {
                        const el = bars[i] as HTMLElement;
                        el.style.height = `${Math.max(2, Math.min(14, levels[i] * 14))}px`;
                        el.style.background = color;
                    }
                });
            }
        };
        return () => {
            onAudioLevelRef.current = null;
            if (frameId) cancelAnimationFrame(frameId);
            levels.fill(0);
        };
    }, [onAudioLevelRef]);

    return (
        <div ref={barsRef} style={{ display: "inline-flex", alignItems: "center", justifyContent: "center", gap: "1px", width: "22px", height: "18px", overflow: "hidden" }} aria-hidden="true">
            {Array.from({ length: NUM_BARS }, (_, i) => <div key={i} style={{ width: "2px", height: "2px", flex: "0 0 2px", borderRadius: "1px", background: themeColor, transition: "height 0.08s ease-out" }} />)}
        </div>
    );
}

export function getWindowControlButtonStyle(t: Theme, variant: "hide" | "fullscreen", active = false): React.CSSProperties {
    const hoverBg = variant === "hide" ? "rgba(148, 163, 184, 0.14)" : `color-mix(in srgb, ${t.btnColor} 14%, transparent)`;
    return {
        ...baseWindowControlBtnStyle,
        color: active ? t.text : t.actionBtnColor,
        background: active ? t.divider : "transparent",
        boxShadow: active ? `inset 0 0 0 1px ${t.fieldBorder}` : "none",
        ['--ai-window-control-hover-bg' as any]: hoverBg,
    };
}

export function windowHideLabel(lang: string) {
    return localizeText(lang, "Hide window", "隐藏窗口", "隱藏窗口");
}

export function windowMaximizeLabel(lang: string, maximized: boolean) {
    return maximized
        ? localizeText(lang, "Restore window", "还原窗口", "還原窗口")
        : localizeText(lang, "Maximize window", "最大化窗口", "最大化窗口");
}

export function stopWindowControlEvent(event: { preventDefault(): void; stopPropagation(): void }) {
    event.preventDefault();
    event.stopPropagation();
}

/** Ignore the second click of a double-click so maximize/restore does not bounce. */
export function toggleWindowOnce(
    event: { preventDefault(): void; stopPropagation(): void; detail: number },
    toggle?: () => void,
) {
    stopWindowControlEvent(event);
    if (event.detail > 1) return;
    toggle?.();
}

/** Pointer hide acts on mousedown so Wails drag cannot swallow it; keyboard hide is a detail-0 click. */
export function hideWindowFromControlEvent(
    event: { type: string; preventDefault(): void; stopPropagation(): void; detail?: number; button?: number },
    hide: () => void,
) {
    stopWindowControlEvent(event);
    if (event.type === "mousedown" && (event.button ?? 0) !== 0) return;
    if (event.type === "click" && (event.detail ?? 0) > 0) return;
    hide();
}
