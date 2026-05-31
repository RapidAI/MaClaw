import { normalizeParticipantId } from "./localAIIdentity";

export function addParticipantIdentityKeys(target: Set<string>, value: unknown) {
    const normalized = normalizeParticipantId(String(value || ""));
    if (!normalized) return;
    target.add(normalized);
    const withoutVEPrefix = /^ve[_-](.+)$/.exec(normalized)?.[1] || normalized;
    target.add(withoutVEPrefix);
    target.add(`ve_${withoutVEPrefix}`);
    target.add(`ve-${withoutVEPrefix}`);
}

export function participantIdentityKeys(...values: unknown[]): string[] {
    const keys = new Set<string>();
    values.forEach((value) => addParticipantIdentityKeys(keys, value));
    return [...keys];
}

export function participantIdentityMatches(left: unknown, right: unknown): boolean {
    const rightKeys = new Set<string>();
    addParticipantIdentityKeys(rightKeys, right);
    if (rightKeys.size === 0) return false;
    const leftKeys = new Set<string>();
    addParticipantIdentityKeys(leftKeys, left);
    for (const key of leftKeys) {
        if (rightKeys.has(key)) return true;
    }
    return false;
}

export function participantNameForIdentity(names: Record<string, string> | undefined | null, id: unknown): string | undefined {
    if (!names) return undefined;
    for (const [key, value] of Object.entries(names)) {
        if (participantIdentityMatches(key, id)) return value;
    }
    return undefined;
}
