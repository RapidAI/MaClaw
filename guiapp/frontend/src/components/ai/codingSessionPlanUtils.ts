/**
 * Helpers for pure-coding session plan extraction / normalization.
 */

/** Collapse whitespace and strip common leading command prefixes. */
export function normalizeSessionPlanCandidate(text: string): string {
    let s = String(text || "").replace(/\r\n/g, "\n").trim();
    if (!s) return "";
    // Drop fenced code blocks — plans should be natural-language goals.
    s = s.replace(/```[\s\S]*?```/g, " ").trim();
    // Prefer the first non-empty paragraph/line block.
    const blocks = s.split(/\n\s*\n/).map((b) => b.trim()).filter(Boolean);
    if (blocks.length > 0) {
        s = blocks[0];
    }
    s = s.replace(/\s+/g, " ").trim();
    // Strip leading slash commands if the whole message is "/cmd ..."
    if (/^\/\w+\b/.test(s)) {
        s = s.replace(/^\/\w+\s*/, "").trim();
    }
    return s;
}

export function truncateSessionPlan(text: string, maxLen = 800): string {
    const s = normalizeSessionPlanCandidate(text);
    if (s.length <= maxLen) return s;
    return s.slice(0, maxLen - 1).trimEnd() + "…";
}

export type SessionPlanMessageLike = {
    role?: string | null;
    content?: string | null;
};

/**
 * Suggest a durable multi-turn session plan from chat history.
 * Prefers the earliest substantial user message (overall goal), falling back
 * to the latest user message when the first is too short/noisy.
 */
export function suggestSessionPlanFromMessages(
    messages: SessionPlanMessageLike[] | null | undefined,
    options?: { minLen?: number; maxLen?: number },
): string {
    const minLen = options?.minLen ?? 8;
    const maxLen = options?.maxLen ?? 800;
    const users = (messages || [])
        .filter((m) => String(m?.role || "").toLowerCase() === "user")
        .map((m) => normalizeSessionPlanCandidate(String(m?.content || "")))
        .filter((s) => s.length >= minLen);
    if (users.length === 0) {
        return "";
    }
    // First substantial user turn is the original goal; if it looks like a
    // one-word ack, prefer the longest early message among the first three.
    const early = users.slice(0, 3);
    let best = early[0];
    for (const candidate of early) {
        if (candidate.length > best.length) best = candidate;
    }
    return truncateSessionPlan(best, maxLen);
}
