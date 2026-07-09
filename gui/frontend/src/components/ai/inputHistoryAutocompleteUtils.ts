/** Max suggestions shown in the history autocomplete list. */
export const INPUT_HISTORY_AUTOCOMPLETE_MAX = 8;

/**
 * Find historical inputs that the current draft is a strict prefix of.
 *
 * - empty draft -> no matches
 * - exact full match of a history item -> excluded (nothing left to complete)
 * - newest history first, de-duplicated
 * - linear scan newest->oldest; stops once `max` unique hits collected
 */
export function matchHistoryPrefix(
    input: string,
    history: readonly string[],
    max = INPUT_HISTORY_AUTOCOMPLETE_MAX,
): string[] {
    if (typeof input !== "string" || !input || max <= 0 || history.length === 0) {
        return [];
    }

    const seen = new Set<string>();
    const out: string[] = [];
    for (let i = history.length - 1; i >= 0; i -= 1) {
        const item = history[i];
        if (typeof item !== "string" || item.length <= input.length) continue;
        if (!item.startsWith(input)) continue;
        if (seen.has(item)) continue;
        seen.add(item);
        out.push(item);
        if (out.length >= max) break;
    }
    return out;
}
