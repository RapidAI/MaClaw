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

/** Normalize ListLansengerGroups Wails payload into bindable survey group rows. */
export function mapLansengerGroupsForSurveyBind(value: unknown): Array<{ group_id: string; name: string }> {
    const parsed = (parseWailsJSON<any>(value) ?? value) as any;
    const list = parsed?.groups || parsed?.Groups || (Array.isArray(parsed) ? parsed : []);
    if (!Array.isArray(list)) return [];
    return list
        .map((x: any) => ({
            group_id: String(x?.group_id || x?.GroupID || x?.groupId || '').trim(),
            name: String(x?.name || x?.Name || x?.group_id || x?.GroupID || x?.groupId || '').trim(),
        }))
        .filter((x: { group_id: string }) => !!x.group_id);
}
