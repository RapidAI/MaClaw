/** Go Wails methods return JSON as string; objects may already be parsed in some runtimes. */
export function parseWailsJSON<T = any>(value: unknown): T {
    if (value == null) {
        return value as T;
    }
    if (typeof value === 'string') {
        const s = value.trim();
        if (!s) return null as T;
        try {
            return JSON.parse(s) as T;
        } catch (e) {
            throw new Error(`invalid JSON from backend: ${(e as Error)?.message || e}`);
        }
    }
    return value as T;
}
