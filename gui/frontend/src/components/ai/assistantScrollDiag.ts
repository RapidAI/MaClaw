// Temporary diagnostics for the AI assistant panel scroll freeze issue.
// All events land in ~/.maclaw/logs/maclaw.log with tag "ai-scroll" via the
// LogFrontendDiagnostic bridge. Field names must avoid the sanitizer's
// dropped keys (anything containing text/content/snippet/prompt/raw), so
// metrics use short names: sh=scrollHeight ch=clientHeight st=scrollTop
// dist=distanceFromBottom tailLen=last message content length.

let seq = 0;
const lastSnapshotAtByEv = new Map<string, number>();
let transitionCount = 0;
let transitionWindowStart = 0;
const MAX_TRANSITIONS_PER_WINDOW = 120;
const TRANSITION_WINDOW_MS = 60_000;

function now(): number {
    return typeof performance !== "undefined" ? performance.now() : Date.now();
}

function sendDiag(ev: string, fields: Record<string, unknown>) {
    const logFrontendDiagnostic = typeof window !== "undefined"
        ? (window as any).go?.main?.App?.LogFrontendDiagnostic
        : undefined;
    if (typeof logFrontendDiagnostic !== "function") return;
    try {
        void Promise.resolve(logFrontendDiagnostic({
            tag: "ai-scroll",
            ev,
            seq: ++seq,
            vis: typeof document !== "undefined" ? document.visibilityState : "?",
            ...fields,
        })).catch(() => { });
    } catch {
        // diagnostics must never affect the panel
    }
}

/** State transitions (latch flips, re-pins, round start/end). Hard-capped. */
export function logAIScrollEvent(ev: string, fields: Record<string, unknown>) {
    const t = Date.now();
    if (t - transitionWindowStart > TRANSITION_WINDOW_MS) {
        transitionWindowStart = t;
        transitionCount = 0;
    }
    if (transitionCount >= MAX_TRANSITIONS_PER_WINDOW) return;
    transitionCount += 1;
    sendDiag(ev, fields);
}

/** Periodic snapshot, emitted at most once per minIntervalMs per event name. */
export function logAIScrollSnapshot(ev: string, fields: Record<string, unknown>, minIntervalMs = 1000) {
    const t = now();
    if (t - (lastSnapshotAtByEv.get(ev) ?? -Infinity) < minIntervalMs) return;
    lastSnapshotAtByEv.set(ev, t);
    sendDiag(ev, fields);
}

// --- stream token pipeline counters (per process, reset on round end) ---
let streamTokens = 0;
let streamChars = 0;
let streamFlushes = 0;

export function noteAIScrollStreamToken(chars: number) {
    streamTokens += 1;
    streamChars += chars;
}

export function noteAIScrollStreamFlush(chars: number) {
    streamFlushes += 1;
    if (streamFlushes % 25 === 0) {
        sendDiag("flush", { tokens: streamTokens, chars: streamChars, flushes: streamFlushes });
    }
}

export function noteAIScrollStreamRoundEnd(reason: string) {
    if (streamTokens === 0 && streamFlushes === 0) return;
    sendDiag("round-end", { reason, tokens: streamTokens, chars: streamChars, flushes: streamFlushes });
    streamTokens = 0;
    streamChars = 0;
    streamFlushes = 0;
}
