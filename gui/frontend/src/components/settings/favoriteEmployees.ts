/** Total sidebar slots for favorite employees (resident + user-configured). */
export const MAX_FAVORITE_EMPLOYEES = 10;

/** User-configurable favorite slots (1 slot is always reserved for resident). */
export const MAX_USER_FAVORITES = 9;

export function normalizeFavoriteEmployeeIds(value: unknown): string[] {
    if (!Array.isArray(value)) return [];
    const seen = new Set<string>();
    const result: string[] = [];
    for (const item of value) {
        const id = String(item || '').trim();
        if (!id || seen.has(id)) continue;
        seen.add(id);
        result.push(id);
        if (result.length >= MAX_USER_FAVORITES) break;
    }
    return result;
}
