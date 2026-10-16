/**
 * ASR transcript helpers shared by voice input and the assistant submit path.
 */

/** Letter or digit — evidence of real speech content (CJK Lo included in \p{L}). */
const ASR_CONTENT_CHAR = /[\p{L}\p{N}]/u;

/**
 * Normalize ASR binding output to a trimmed string.
 * Wails usually returns string; mocks/edge cases may yield null/undefined.
 */
export function normalizeASRText(text: unknown): string {
    if (typeof text === "string") return text.trim();
    if (text == null) return "";
    return String(text).trim();
}

/**
 * True when ASR text is empty or has no letters/digits (only punctuation,
 * symbols, or whitespace). Continuous ASR often emits lone "。" / "." / "…"
 * for breath or noise — those must not reach the agent or pre-send queue.
 *
 * Aligns with backend shouldDropASRText "punctuation-only" (unicode.IsLetter/IsDigit).
 */
export function isPunctuationOnlyASRText(text: unknown): boolean {
    const trimmed = normalizeASRText(text);
    if (!trimmed) return true;
    return !ASR_CONTENT_CHAR.test(trimmed);
}

/** True when transcript should be sent to agent / pre-send queue. */
export function shouldDispatchASRText(text: unknown): boolean {
    return !isPunctuationOnlyASRText(text);
}

/**
 * Pick the text to send after optional LLM voice-command normalization.
 * Continuous mode drops non-commands; never drops good speech because a
 * correction came back empty/punctuation-only (fall back to raw ASR).
 */
export function resolveNormalizedVoiceText(
    raw: string,
    norm: { is_command?: boolean; corrected_text?: string } | null | undefined,
    source: "hold" | "continuous",
): { dispatch: boolean; text: string; reason: string } {
    const input = normalizeASRText(raw);
    if (!shouldDispatchASRText(input)) {
        return { dispatch: false, text: "", reason: "empty_or_punctuation" };
    }
    if (!norm) {
        return { dispatch: true, text: input, reason: "no_normalization" };
    }
    // Explicit false only — missing is_command (older clients) means "keep".
    const isCommand = norm.is_command !== false;
    if (source === "continuous" && !isCommand) {
        return { dispatch: false, text: "", reason: "not_a_command" };
    }
    // Prefer a *usable* correction. Empty or punctuation-only corrections must
    // not discard good original ASR (aligns with backend correctASRText).
    const hasCorrectedField = typeof norm.corrected_text === "string";
    const correctedRaw = hasCorrectedField ? normalizeASRText(norm.corrected_text) : "";
    const useCorrected = correctedRaw !== "" && shouldDispatchASRText(correctedRaw);
    const text = useCorrected ? correctedRaw : input;
    const changed = useCorrected && text !== input;
    return {
        dispatch: true,
        text,
        reason: changed
            ? "corrected"
            : (isCommand ? "unchanged" : "hold_raw_or_corrected"),
    };
}
