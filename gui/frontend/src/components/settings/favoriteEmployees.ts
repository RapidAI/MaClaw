export const MAX_FAVORITE_EMPLOYEES = 12;

export function normalizeFavoriteEmployeeIds(value: unknown): string[] {
    if (!Array.isArray(value)) return [];
    const seen = new Set<string>();
    const result: string[] = [];
    for (const item of value) {
        const id = String(item || '').trim();
        if (!id || seen.has(id)) continue;
        seen.add(id);
        result.push(id);
        if (result.length >= MAX_FAVORITE_EMPLOYEES) break;
    }
    return result;
}
