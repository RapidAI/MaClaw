import type { VirtualEmployeeEntry } from '../components/ai/VirtualEmployeeTab';
import { participantIdentityKeys, participantIdentityMatches } from '../components/ai/participantIdentity';
import { isVirtualEmployeeOnline } from '../components/ai/virtualEmployeeStatus';

export type VirtualEmployeeEventPatch = Partial<VirtualEmployeeEntry> & { id?: string; machine_id?: string };

export function virtualEmployeeIdForMachine(machineId: string): string {
    const cleaned = String(machineId || '').trim().replace(/[\\/ ]/g, '_');
    return cleaned ? `ve_${cleaned}` : '';
}

export function isOwnVirtualEmployeeId(id: string, machineId?: string): boolean {
    const normalizedId = String(id || '').trim().toLowerCase();
    const normalizedMachineId = String(machineId || '').trim().toLowerCase();
    if (!normalizedId || !normalizedMachineId) return false;
    return normalizedId === normalizedMachineId || normalizedId === virtualEmployeeIdForMachine(normalizedMachineId).toLowerCase();
}

export function normalizeFavoriteEmployeeNames(value: unknown): Record<string, string> {
    if (!value || typeof value !== 'object' || Array.isArray(value)) return {};
    return Object.entries(value as Record<string, unknown>).reduce<Record<string, string>>((acc, [key, rawName]) => {
        const id = String(key || '').trim();
        const name = String(rawName || '').trim();
        if (id && name) acc[id] = name;
        return acc;
    }, {});
}

export function favoriteEmployeeAliasIds(veId: string, veList: VirtualEmployeeEntry[]): string[] {
    const aliases = new Set<string>();
    const add = (value?: string) => {
        const id = String(value || '').trim();
        if (!id) return;
        aliases.add(id);
        participantIdentityKeys(id).forEach(key => aliases.add(key));
    };
    add(veId);
    const ve = veList.find(v => participantIdentityMatches(v.id, veId) || participantIdentityMatches(v.machine_id, veId));
    add(ve?.id);
    add(ve?.machine_id);
    return Array.from(aliases);
}

export function favoriteEmployeeIDInAliasSet(id: string, aliases: Set<string>): boolean {
    const normalized = String(id || '').trim();
    if (!normalized) return false;
    if (aliases.has(normalized)) return true;
    return participantIdentityKeys(normalized).some(key => aliases.has(key));
}

export function filterOnlineVirtualEmployees(list: VirtualEmployeeEntry[]): VirtualEmployeeEntry[] {
    return Array.isArray(list) ? list.filter(isVirtualEmployeeOnline) : [];
}

function readStringField(source: Record<string, any>, ...keys: string[]): string | undefined {
    for (const key of keys) {
        if (Object.prototype.hasOwnProperty.call(source, key)) return String(source[key] || '').trim();
    }
    return undefined;
}

function normalizeVEEventOnlineStatus(value: string | undefined): "online" | "offline" | undefined {
    const normalized = String(value || '').trim().toLowerCase();
    if (normalized === 'offline') return 'offline';
    if (normalized === 'online') return 'online';
    return undefined;
}

function readArrayField(source: Record<string, any>, ...keys: string[]): string[] | undefined {
    for (const key of keys) {
        if (Object.prototype.hasOwnProperty.call(source, key)) return Array.isArray(source[key]) ? source[key] : [];
    }
    return undefined;
}

export function virtualEmployeeFromEventPayload(eventData: any): VirtualEmployeeEventPatch | null {
    const payload = eventData?.payload && typeof eventData.payload === 'object' ? eventData.payload : eventData;
    const employee = payload?.employee || payload?.Employee || payload?.virtual_employee || payload?.ve;
    if (!employee || typeof employee !== 'object') {
        const root = eventData && typeof eventData === 'object' ? eventData : {};
        const source = payload && typeof payload === 'object' ? payload : {};
        const id = readStringField(source, 've_id', 'veId', 'id', 'ID') || readStringField(root, 've_id', 'veId', 'id', 'ID') || '';
        const machineId = readStringField(source, 'machine_id', 'machineId', 'MachineID') || readStringField(root, 'machine_id', 'machineId', 'MachineID') || '';
        if (!id && !machineId) return null;
        const patch: VirtualEmployeeEventPatch = { id, machine_id: machineId };
        const onlineStatus = normalizeVEEventOnlineStatus(
            readStringField(source, 'online_status', 'onlineStatus', 'OnlineStatus', 'status', 'Status') ||
            readStringField(root, 'online_status', 'onlineStatus', 'OnlineStatus', 'status', 'Status')
        );
        if (onlineStatus) patch.online_status = onlineStatus;
        return patch;
    }
    const id = readStringField(employee, 'id', 'ID') || '';
    const machineId = readStringField(employee, 'machine_id', 'MachineID') || '';
    if (!id && !machineId) return null;
    const patch: VirtualEmployeeEventPatch = { id, machine_id: machineId };
    const name = readStringField(employee, 'name', 'Name');
    if (name !== undefined) patch.name = name;
    const skillDescription = readStringField(employee, 'skill_description', 'SkillDescription');
    if (skillDescription !== undefined) patch.skill_description = skillDescription;
    const avatarDataURL = readStringField(employee, 'avatar_data_url', 'AvatarDataURL');
    if (avatarDataURL !== undefined) patch.avatar_data_url = avatarDataURL;
    const rawPolicy = readStringField(employee, 'access_policy', 'AccessPolicy');
    if (rawPolicy !== undefined) patch.access_policy = rawPolicy === 'whitelist' || rawPolicy === 'blacklist' || rawPolicy === 'per_request' ? rawPolicy : 'public';
    const status = readStringField(employee, 'status', 'Status');
    if (status !== undefined) patch.status = status;
    const rawOnlineStatus = readStringField(employee, 'online_status', 'OnlineStatus')?.toLowerCase();
    if (rawOnlineStatus !== undefined) patch.online_status = rawOnlineStatus === 'offline' ? 'offline' : 'online';
    if (Object.prototype.hasOwnProperty.call(employee, 'resident') || Object.prototype.hasOwnProperty.call(employee, 'Resident')) {
        patch.resident = Boolean(employee.resident || employee.Resident);
    }
    const registeredAt = readStringField(employee, 'registered_at', 'RegisteredAt');
    if (registeredAt !== undefined) patch.registered_at = registeredAt;
    const whitelist = readArrayField(employee, 'whitelist', 'Whitelist');
    if (whitelist !== undefined) patch.whitelist = whitelist;
    return patch;
}

export function completeVirtualEmployeeEntry(next: VirtualEmployeeEventPatch): VirtualEmployeeEntry {
    const id = String(next.id || next.machine_id || '').trim();
    return {
        id,
        machine_id: next.machine_id,
        name: next.name || id.slice(0, 8),
        skill_description: next.skill_description || '',
        avatar_data_url: next.avatar_data_url,
        access_policy: next.access_policy || 'public',
        status: next.status || 'active',
        online_status: next.online_status || 'online',
        resident: next.resident,
        registered_at: next.registered_at,
        whitelist: next.whitelist,
    };
}

export function mergeVirtualEmployeeEntry(current: VirtualEmployeeEntry, next: VirtualEmployeeEventPatch): VirtualEmployeeEntry {
    const merged = { ...current };
    (['id', 'machine_id', 'name', 'skill_description', 'avatar_data_url', 'access_policy', 'status', 'online_status', 'resident', 'registered_at', 'whitelist'] as const).forEach((key) => {
        if (next[key] !== undefined) (merged as any)[key] = next[key];
    });
    return merged;
}

export function mergeVirtualEmployeeList(prev: VirtualEmployeeEntry[], next: VirtualEmployeeEventPatch): VirtualEmployeeEntry[] {
    if (!next) return prev;
    const nextId = String(next.id || '').trim();
    const nextMachineId = String(next.machine_id || '').trim();
    const index = prev.findIndex(item => participantIdentityMatches(item.id, nextId) || participantIdentityMatches(item.machine_id, nextId) || participantIdentityMatches(item.id, nextMachineId) || participantIdentityMatches(item.machine_id, nextMachineId));
    if (index < 0) return next.online_status === 'offline' ? prev : [...prev, completeVirtualEmployeeEntry(next)];
    const merged = [...prev];
    merged[index] = mergeVirtualEmployeeEntry(merged[index], next);
    return merged;
}

export function virtualEmployeeOverrideKey(employee: VirtualEmployeeEventPatch): string {
    return String(employee.machine_id || employee.id || '').trim();
}

export function applyVirtualEmployeeOverrides(list: VirtualEmployeeEntry[], overrides: Map<string, { employee: VirtualEmployeeEventPatch; expiresAt: number }>, now = Date.now()): VirtualEmployeeEntry[] {
    let next = list;
    overrides.forEach((entry, key) => {
        if (entry.expiresAt <= now) {
            overrides.delete(key);
            return;
        }
        next = mergeVirtualEmployeeList(next, entry.employee);
    });
    return next;
}

export function residentVirtualEmployeeAliases(ve: VirtualEmployeeEntry): string[] {
    return favoriteEmployeeAliasIds(String(ve.machine_id || ve.id || '').trim() || ve.id, [ve]);
}
