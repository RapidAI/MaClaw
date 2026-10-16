/**
 * Unwraps an error coming back from a Wails call (a plain string, or an object
 * carrying `message` / `error` / `data`, possibly nested) into a displayable
 * message, falling back to the caller-supplied text.
 */
export function formatCodingKnowledgeActionError(err: unknown, fallback: string, depth = 0): string {
    if (depth > 3) return fallback;
    if (typeof err === 'string' && err.trim()) return err.trim();
    if (err && typeof err === 'object') {
        const record = err as Record<string, unknown>;
        for (const key of ['message', 'error', 'data']) {
            const value = record[key];
            if (typeof value === 'string' && value.trim()) return value.trim();
            if (value && typeof value === 'object') {
                const nested = formatCodingKnowledgeActionError(value, '', depth + 1);
                if (nested) return nested;
            }
        }
    }
    const text = err == null ? '' : String(err).trim();
    return text && text !== '[object Object]' ? text : fallback;
}
