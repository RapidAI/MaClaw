import type { ReactNode } from "react";
import { useEffect, useRef } from "react";
import type { ChatMessage } from "./useAIAssistant";
import type { Theme } from "./aiAssistantPanelTheme";
import { AssistantPinnedNewsCards } from "./AssistantPinnedNewsCards";

/** Synthesize a phone-boot-style startup sound: majestic arpeggio with metallic touch. */
function playStartupChime() {
    try {
        const ctx = new AudioContext();
        const now = ctx.currentTime;

        // Master gain
        const master = ctx.createGain();
        master.gain.setValueAtTime(0.45, now);
        master.connect(ctx.destination);

        // Ascending arpeggio: C5-E5-G5-C6 with metallic bell character
        const arp: [number, number][] = [[523, 0], [659, 0.45], [784, 0.9], [1047, 1.35]];
        for (const [freq, start] of arp) {
            // Fundamental (sine)
            const osc = ctx.createOscillator();
            const g = ctx.createGain();
            osc.type = 'sine';
            osc.frequency.setValueAtTime(freq, now + start);
            g.gain.setValueAtTime(0, now + start);
            g.gain.linearRampToValueAtTime(0.10, now + start + 0.06);
            g.gain.exponentialRampToValueAtTime(0.001, now + start + 1.8);
            osc.connect(g).connect(master);
            osc.start(now + start);
            osc.stop(now + start + 1.9);

            // Brightness (triangle octave)
            const osc2 = ctx.createOscillator();
            const g2 = ctx.createGain();
            osc2.type = 'triangle';
            osc2.frequency.setValueAtTime(freq * 2, now + start);
            g2.gain.setValueAtTime(0, now + start);
            g2.gain.linearRampToValueAtTime(0.02, now + start + 0.06);
            g2.gain.exponentialRampToValueAtTime(0.001, now + start + 1.5);
            osc2.connect(g2).connect(master);
            osc2.start(now + start);
            osc2.stop(now + start + 1.6);

            // Metallic partial (2.76x, fast decay)
            const osc3 = ctx.createOscillator();
            const g3 = ctx.createGain();
            osc3.type = 'sine';
            osc3.frequency.setValueAtTime(freq * 2.76, now + start);
            g3.gain.setValueAtTime(0.018, now + start);
            g3.gain.exponentialRampToValueAtTime(0.001, now + start + 0.25);
            osc3.connect(g3).connect(master);
            osc3.start(now + start);
            osc3.stop(now + start + 0.3);

            // Attack transient (noise burst)
            const bufSize = Math.floor(ctx.sampleRate * 0.01);
            const noiseBuf = ctx.createBuffer(1, bufSize, ctx.sampleRate);
            const data = noiseBuf.getChannelData(0);
            for (let j = 0; j < bufSize; j++) data[j] = (Math.random() * 2 - 1) * 0.5;
            const noise = ctx.createBufferSource();
            const ng = ctx.createGain();
            const nf = ctx.createBiquadFilter();
            noise.buffer = noiseBuf;
            nf.type = 'bandpass';
            nf.frequency.setValueAtTime(freq * 3.2, now + start);
            nf.Q.setValueAtTime(6, now + start);
            ng.gain.setValueAtTime(0.08, now + start);
            ng.gain.exponentialRampToValueAtTime(0.001, now + start + 0.012);
            noise.connect(nf).connect(ng).connect(master);
            noise.start(now + start);
            noise.stop(now + start + 0.015);
        }

        // Final resolve chord (C6-E6-G6), clean
        const resolveStart = 1.9;
        for (const freq of [1047, 1319, 1568]) {
            const osc = ctx.createOscillator();
            const g = ctx.createGain();
            osc.type = 'sine';
            osc.frequency.setValueAtTime(freq, now + resolveStart);
            g.gain.setValueAtTime(0, now + resolveStart);
            g.gain.linearRampToValueAtTime(0.06, now + resolveStart + 0.15);
            g.gain.exponentialRampToValueAtTime(0.001, now + resolveStart + 1.8);
            osc.connect(g).connect(master);
            osc.start(now + resolveStart);
            osc.stop(now + resolveStart + 1.9);
        }

        // Cleanup
        setTimeout(() => ctx.close(), 4500);
    } catch {
        // Audio not available; silent fallback
    }
}

interface AssistantConversationBodyProps {
    emptyContent?: ReactNode;
    initLabel: string;
    lang: string;
    messages: ChatMessage[];
    onOpenOnboarding?: () => void;
    onboardingIncomplete?: boolean;
    pinnedNews: ChatMessage[];
    processingText: string;
    ready: boolean;
    renderedOtherMessages: ReactNode;
    renderedProgressMessages: ReactNode;
    showProcessingState: boolean;
    showThinkingState: boolean;
    theme: Theme;
    thinkingText: string;
}

export function AssistantConversationBody({
    emptyContent,
    initLabel,
    lang,
    messages,
    onOpenOnboarding,
    onboardingIncomplete,
    pinnedNews,
    processingText,
    ready,
    renderedOtherMessages,
    renderedProgressMessages,
    showProcessingState,
    showThinkingState,
    theme: t,
    thinkingText,
}: AssistantConversationBodyProps) {
    // Play startup chime once when brand animation is shown
    const chimePlayedRef = useRef(false);
    useEffect(() => {
        if (!ready && !onboardingIncomplete && !chimePlayedRef.current) {
            chimePlayedRef.current = true;
            // Small delay to let the UI render first
            const timer = setTimeout(() => playStartupChime(), 200);
            return () => clearTimeout(timer);
        }
    }, [ready, onboardingIncomplete]);

    return (
        <>
            {onboardingIncomplete ? (
                <div style={{ display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center", height: "100%", gap: "16px" }}>
                    <div style={{ color: t.textMuted, fontSize: "13px" }}>
                        {lang === "en" ? "Setup not completed" : "\u8bbe\u7f6e\u672a\u5b8c\u6210"}
                    </div>
                    <button
                        onClick={onOpenOnboarding}
                        style={{ padding: "10px 28px", fontSize: "15px", fontWeight: 600, background: "#2f6fbc", color: "#fff", border: "1px solid #2f6fbc", borderRadius: "8px", cursor: "pointer", transition: "opacity 0.2s" }}
                        onMouseEnter={e => (e.currentTarget.style.opacity = "0.85")}
                        onMouseLeave={e => (e.currentTarget.style.opacity = "1")}
                    >
                        {lang === "en" ? "Complete Setup" : "\u5b8c\u6210\u8bbe\u7f6e"}
                    </button>
                </div>
            ) : !ready ? (
                <div style={{ display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center", height: "100%", gap: "18px" }}>
                    <div style={{ display: "flex", alignItems: "baseline", gap: "7px", animation: "maclaw-brand-breathe 2.4s ease-in-out infinite" }}>
                        <span style={{ fontSize: "28px", fontWeight: 700, color: t.text, letterSpacing: "0" }}>码卡龙</span>
                        <span className="brand-version-mark" style={{ fontSize: '30px' }}>7</span>
                        <span style={{ fontSize: "21px", fontWeight: 650, color: '#7a2330', letterSpacing: '0.04em' }}>万变</span>
                    </div>
                    <div style={{ color: t.textMuted, fontSize: "11px", opacity: 0.7 }}>{initLabel}</div>
                </div>
            ) : messages.length === 0 ? (
                emptyContent !== undefined ? emptyContent : (
                    <span style={{ color: t.emptyHint }}>
                        {lang === "en" ? "Ask me anything..." : "\u6709\u4ec0\u4e48\u53ef\u4ee5\u5e2e\u4f60\u7684\uff1f"}
                    </span>
                )
            ) : (
                <>
                    <AssistantPinnedNewsCards messages={pinnedNews} theme={t} />
                    {renderedOtherMessages}
                    {renderedProgressMessages}
                </>
            )}
            {showThinkingState && <div style={{ color: t.textMuted, fontSize: "11px", padding: "4px 0", fontStyle: "italic" }}>{thinkingText}</div>}
            {showProcessingState && <div style={{ color: t.textMuted, fontSize: "11px", padding: "4px 0", fontStyle: "italic" }}>{processingText}</div>}
        </>
    );
}
