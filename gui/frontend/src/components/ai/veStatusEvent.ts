import { normalizeParticipantId } from "./localAIIdentity";

export type VEStatusEventInfo = {
    ids: string[];
    status: string;
};

export function veStatusEventInfo(data: any): VEStatusEventInfo {
    const payload = data?.payload || data || {};
    const employee = payload?.employee || payload?.Employee || data?.employee || data?.Employee || {};
    const ids = uniqueNormalizedIds([
        data?.ve_id,
        data?.veId,
        data?.id,
        payload?.ve_id,
        payload?.veId,
        payload?.id,
        employee?.ve_id,
        employee?.veId,
        employee?.id,
        employee?.ID,
        data?.machine_id,
        data?.machineId,
        data?.MachineID,
        payload?.machine_id,
        payload?.machineId,
        payload?.MachineID,
        employee?.machine_id,
        employee?.machineId,
        employee?.MachineID,
    ]);
    const status = String(
        data?.online_status || data?.onlineStatus || data?.OnlineStatus || data?.status || data?.Status ||
        payload?.online_status || payload?.onlineStatus || payload?.OnlineStatus || payload?.status || payload?.Status ||
        employee?.online_status || employee?.onlineStatus || employee?.OnlineStatus || employee?.status || employee?.Status || ""
    ).trim().toLowerCase();
    return { ids, status };
}

export function veStatusEventMatches(data: any, id: string): boolean {
    const normalized = normalizeParticipantId(id);
    if (!normalized) return false;
    return veStatusEventInfo(data).ids.includes(normalized);
}

function uniqueNormalizedIds(values: unknown[]): string[] {
    const out: string[] = [];
    const seen = new Set<string>();
    for (const value of values) {
        const id = normalizeParticipantId(String(value || ""));
        if (!id || seen.has(id)) continue;
        seen.add(id);
        out.push(id);
    }
    return out;
}
