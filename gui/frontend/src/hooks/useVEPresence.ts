/**
 * useVEPresence — 数字员工在线状态的单一数据源 Hook
 *
 * 机制：
 * 1. 定期轮询 ListVirtualEmployees（正常 30s，Hub 不可达时 10s）
 * 2. WebSocket 事件驱动刷新（ve:status_change / ve:list_update）
 * 3. 客户端侧过期降级（60s 无刷新 → unknown）
 * 4. Visibility API 集成（后台暂停，前台恢复时立即拉取）
 */

import { useCallback, useEffect, useRef, useState } from "react";
import { EventsOn, EventsOff } from "../../wailsjs/runtime";
import type { VirtualEmployeeEntry } from "../components/ai/VirtualEmployeeTab";
import { isVirtualEmployeeOnline } from "../components/ai/virtualEmployeeStatus";
import type { VEOnlineStatus } from "../components/ai/VEStatusDot";

// --- Constants ---
const POLL_INTERVAL_NORMAL = 30_000;   // 30s
const POLL_INTERVAL_DEGRADED = 10_000; // 10s when Hub unreachable
const STALE_THRESHOLD = 60_000;        // 60s → unknown
const TICK_INTERVAL = 15_000;          // 15s re-render tick for expiry check

// --- Types ---
export interface VEPresenceInfo {
    hubStatus: "online" | "offline";
    fetchedAt: number;
}

function presenceLookupIDs(ve: Pick<VirtualEmployeeEntry, "id" | "machine_id">): string[] {
    return Array.from(new Set([ve.id, ve.machine_id].map((id) => String(id || "").trim()).filter(Boolean)));
}

export interface UseVEPresenceReturn {
    /** Full VE list (latest successful fetch) */
    veList: VirtualEmployeeEntry[];
    /** Get effective status for a VE by id */
    getStatus: (veId: string) => VEOnlineStatus;
    /** Whether the overall data is stale (Hub unreachable) */
    isStale: boolean;
    /** Timestamp of last successful fetch */
    lastFetchAt: number;
    /** Monotonically increasing version — changes on fetch and on tick (for useMemo deps) */
    version: number;
}

export interface UseVEPresenceConfig {
    /** Whether Hub is configured (has URL + machine ID) */
    hubConfigured: boolean;
    /** Optional override for testing */
    listVirtualEmployees?: () => Promise<VirtualEmployeeEntry[]>;
}

export function useVEPresence({ hubConfigured, listVirtualEmployees }: UseVEPresenceConfig): UseVEPresenceReturn {
    const [veList, setVeList] = useState<VirtualEmployeeEntry[]>([]);
    const [lastFetchAt, setLastFetchAt] = useState<number>(0);
    // Version counter — increments on fetch and on tick, used by consumers in useMemo deps
    const [version, setVersion] = useState(0);

    // Use refs for mutable state that shouldn't trigger effect re-runs
    const presenceMapRef = useRef<Map<string, VEPresenceInfo>>(new Map());
    const hubReachableRef = useRef<boolean>(true);
    const skipNextPoll = useRef(false);
    const pollTimerRef = useRef<number | undefined>(undefined);
    const fetchingRef = useRef(false);
    const listFnRef = useRef(listVirtualEmployees);

    // Keep listFnRef in sync without triggering effects
    useEffect(() => { listFnRef.current = listVirtualEmployees; }, [listVirtualEmployees]);

    // --- Fetch logic (stable reference — no deps that change) ---
    const fetchVeList = useCallback(async () => {
        if (fetchingRef.current) return;
        fetchingRef.current = true;
        try {
            let fn = listFnRef.current;
            if (!fn) {
                const mod = await import("../../wailsjs/go/main/App");
                fn = (mod as any).ListVirtualEmployees;
            }
            if (!fn) {
                hubReachableRef.current = false;
                return;
            }
            const list: VirtualEmployeeEntry[] = await fn();
            const now = Date.now();
            setVeList(list || []);
            hubReachableRef.current = true;
            setLastFetchAt(now);
            setVersion(v => v + 1);

            // Build presence map (mutate ref, no state update needed)
            const newMap = new Map<string, VEPresenceInfo>();
            for (const ve of (list || [])) {
                const info: VEPresenceInfo = {
                    hubStatus: isVirtualEmployeeOnline(ve) ? "online" : "offline",
                    fetchedAt: now,
                };
                for (const id of presenceLookupIDs(ve)) {
                    newMap.set(id, info);
                }
            }
            presenceMapRef.current = newMap;
        } catch {
            hubReachableRef.current = false;
        } finally {
            fetchingRef.current = false;
        }
    // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    // --- Polling scheduler (single effect, stable deps) ---
    useEffect(() => {
        if (!hubConfigured) return;

        let cancelled = false;

        const scheduleNext = () => {
            if (cancelled) return;
            const interval = hubReachableRef.current ? POLL_INTERVAL_NORMAL : POLL_INTERVAL_DEGRADED;
            pollTimerRef.current = window.setTimeout(() => {
                if (cancelled) return;
                if (document.hidden) {
                    scheduleNext();
                    return;
                }
                if (skipNextPoll.current) {
                    skipNextPoll.current = false;
                    scheduleNext();
                    return;
                }
                fetchVeList().finally(() => { if (!cancelled) scheduleNext(); });
            }, interval);
        };

        // Initial fetch
        fetchVeList().finally(() => { if (!cancelled) scheduleNext(); });

        return () => {
            cancelled = true;
            if (pollTimerRef.current !== undefined) {
                window.clearTimeout(pollTimerRef.current);
            }
        };
    }, [hubConfigured, fetchVeList]);

    // --- WebSocket event listeners ---
    useEffect(() => {
        if (!hubConfigured) return;

        const handleEvent = () => {
            skipNextPoll.current = true;
            fetchVeList();
        };

        const unsub1 = EventsOn("ve:status_change", handleEvent);
        const unsub2 = EventsOn("ve:list_update", handleEvent);

        return () => {
            if (typeof unsub1 === "function") unsub1(); else EventsOff("ve:status_change");
            if (typeof unsub2 === "function") unsub2(); else EventsOff("ve:list_update");
        };
    }, [hubConfigured, fetchVeList]);

    // --- Visibility API: fetch immediately when returning to foreground ---
    useEffect(() => {
        if (!hubConfigured) return;

        const handler = () => {
            if (!document.hidden) {
                skipNextPoll.current = true;
                fetchVeList();
            }
        };
        document.addEventListener("visibilitychange", handler);
        return () => document.removeEventListener("visibilitychange", handler);
    }, [hubConfigured, fetchVeList]);

    // --- Tick for expiry re-evaluation ---
    useEffect(() => {
        const id = window.setInterval(() => setVersion(v => v + 1), TICK_INTERVAL);
        return () => window.clearInterval(id);
    }, []);

    // --- Status computation (reads from ref — stable identity) ---
    const getStatus = useCallback((veId: string): VEOnlineStatus => {
        if (!hubReachableRef.current) return "unknown";
        const info = presenceMapRef.current.get(veId);
        if (!info) return "unknown";
        if (Date.now() - info.fetchedAt > STALE_THRESHOLD) return "unknown";
        return info.hubStatus;
    }, []);

    const isStale = !hubReachableRef.current || (lastFetchAt > 0 && Date.now() - lastFetchAt > STALE_THRESHOLD);

    return { veList, getStatus, isStale, lastFetchAt, version };
}
