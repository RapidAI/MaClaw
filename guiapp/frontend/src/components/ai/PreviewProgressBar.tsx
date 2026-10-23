import { useEffect, type CSSProperties } from "react";

/** Short previews finish before this and never flash a bar. */
export const PREVIEW_BAR_DELAY_MS = 320;

/**
 * At this size, SheetJS or docx-preview can block the UI thread long enough
 * that a timer scheduled before the call will not paint. Show the bar first.
 */
export const PREVIEW_HEAVY_PARSE_BYTES = 1 << 20;

export type PreviewPhase = "wait" | "read" | "parse";

export type PreviewProgress = {
    phase: PreviewPhase;
    /** Null is indeterminate. A finite 0–1 is received/total. */
    ratio: number | null;
};

export type PreviewProgressPhrases = {
    wait: string;
    read: string;
    parse: string;
};

const PREVIEW_PROGRESS_CSS = `
@keyframes maclaw-preview-progress-slide {
  0% { transform: translateX(-120%); }
  100% { transform: translateX(320%); }
}
.maclaw-preview-progress-slide {
  animation: maclaw-preview-progress-slide 1.05s ease-in-out infinite;
}
@media (prefers-reduced-motion: reduce) {
  .maclaw-preview-progress-slide {
    animation: none !important;
    transform: none !important;
    width: 100% !important;
    opacity: 0.5;
  }
}
`;

export function previewProgressSentence(phase: PreviewPhase, phrases: PreviewProgressPhrases): string {
    return phase === "parse" ? phrases.parse : phase === "read" ? phrases.read : phrases.wait;
}

/** Parsing has no byte total. A missing length stays unlabeled. */
export function previewProgressPercent(phase: PreviewPhase, ratio: number | null): number | null {
    if (phase === "parse" || typeof ratio !== "number" || !Number.isFinite(ratio)) return null;
    return Math.max(0, Math.min(100, Math.round(ratio * 100)));
}

export function previewProgressLabel(phase: PreviewPhase, ratio: number | null, phrases: PreviewProgressPhrases): string {
    const sentence = previewProgressSentence(phase, phrases);
    const pct = previewProgressPercent(phase, ratio);
    return pct == null ? sentence : `${sentence} ${pct}%`;
}

/** Let the loading state commit before a synchronous parse. */
export function yieldPreviewPaint(): Promise<void> {
    return new Promise((resolve) => {
        setTimeout(resolve, 0);
    });
}

function ensurePreviewProgressCSS() {
    if (typeof document === "undefined") return;
    if (document.head.querySelector("style[data-maclaw-preview-progress]")) return;
    const style = document.createElement("style");
    style.setAttribute("data-maclaw-preview-progress", "");
    style.textContent = PREVIEW_PROGRESS_CSS;
    document.head.appendChild(style);
}

/**
 * Determinate while bytes are arriving, indeterminate while a document is
 * parsed. The app theme paints `[role=progressbar]`; the inline colors cover
 * a screen that has not loaded that sheet.
 */
export function PreviewProgressBar({ label, ratio }: { label: string; ratio: number | null }) {
    const determinate = typeof ratio === "number" && Number.isFinite(ratio);
    const pct = determinate ? Math.max(0, Math.min(100, Math.round((ratio as number) * 100))) : 0;
    useEffect(() => {
        ensurePreviewProgressCSS();
    }, []);
    return (
        <div style={{ marginTop: 10, width: "min(280px, 100%)" }}>
            <div
                role="progressbar"
                aria-label={label}
                aria-valuemin={0}
                aria-valuemax={100}
                aria-valuenow={determinate ? pct : undefined}
                data-testid="preview-progress-bar"
                data-mode={determinate ? "determinate" : "indeterminate"}
                style={{
                    height: 6,
                    overflow: "hidden",
                    borderRadius: 999,
                    background: "var(--office-accent-soft, rgba(37, 99, 235, 0.16))",
                }}
            >
                <div
                    className={determinate ? undefined : "maclaw-preview-progress-slide"}
                    style={{
                        height: "100%",
                        width: determinate ? `${pct}%` : "36%",
                        borderRadius: 999,
                        background: "var(--office-accent, #2563eb)",
                    }}
                />
            </div>
        </div>
    );
}

/**
 * The phase sentence is the live region. Byte ticks stay beside it, hidden
 * from that region: `role="status"` is atomic, so a percent inside it is
 * spoken again on every update. The bar carries the number on `aria-valuenow`.
 */
export function PreviewLoadingStatus({
    testId,
    style,
    phase,
    ratio,
    phrases,
    showBar,
}: {
    testId: string;
    style?: CSSProperties;
    phase: PreviewPhase;
    ratio: number | null;
    phrases: PreviewProgressPhrases;
    showBar: boolean;
}) {
    const sentence = previewProgressSentence(phase, phrases);
    const percent = previewProgressPercent(phase, ratio);
    return (
        <div data-testid={testId} style={style}>
            <div>
                <span role="status">{sentence}</span>
                {percent != null ? <span aria-hidden="true">{` ${percent}%`}</span> : null}
            </div>
            {showBar ? <PreviewProgressBar label={sentence} ratio={phase === "parse" ? null : ratio} /> : null}
        </div>
    );
}
