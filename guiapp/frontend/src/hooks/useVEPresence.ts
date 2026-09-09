/**
 * useVEPresence - shared digital employee presence source.
 *
 * Polls ListVirtualEmployees with jitter/backoff, coalesces websocket bursts,
 * degrades stale data to unknown, and pauses polling while the app is hidden.
 */

import { useCallback, useEffect, useRef, useState } from "react";
import { EventsOn, EventsOff } from "../../wailsjs/runtime";
import type { VirtualEmployeeEntry } from "../components/ai/VirtualEmployeeTab";
import { isVirtualEmployeeOnline } from "../components/ai/virtualEmployeeStatus";
import type { VEOnlineStatus } from "../components/ai/VEStatusDot";
import { participantIdentityKeys, participantIdentityMatches } from "../components/ai/participantIdentity";
import { veStatusEventInfo } from "../components/ai/veStatusEvent";
import { getWailsAppModule } from "../utils/wailsAppModule";

// --- Constants ---
const POLL_INTERVAL_NORMAL = 45_000;
const POLL_INTERVAL_MAX = 180_000;
const POLL_JITTER_MS = 10_000;
const EVENT_REFRESH_THROTTLE_MS = 1_500;
const STALE_THRESHOLD = 60_000;        // 60s → unknown
const TICK_INTERVAL = 15_000;          // 15s re-render tick for expiry check

// --- Types ---
export interface VEPresenceInfo {
    hubStatus: "online" | "offline";
    fetchedAt: number;
}

function presenceLookupIDs(ve: Pick<VirtualEmployeeEntry, "id" | "machine_id">): string[] {
    return participantIdentityKeys(ve.id, ve.machine_id);
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
    const mountedRef = useRef(true);
    const pollingActiveRef = useRef(false);
    const pollingEpochRef = useRef(0);
    const skipNextPoll = useRef(false);
    const pollTimerRef = useRef<number | undefined>(undefined);
    const eventThrottleRef = useRef<number | undefined>(undefined);
    const pendingEventRefreshRef = useRef(false);
    const fetchingRef = useRef(false);
    const fetchAgainRef = useRef(false);
    const consecutiveFailuresRef = useRef(0);
    const listFnRef = useRef(listVirtualEmployees);

    // Keep listFnRef in sync without triggering effects
    useEffect(() => { listFnRef.current = listVirtualEmployees; }, [listVirtualEmployees]);

    useEffect(() => {
        mountedRef.current = true;
        return () => {
            mountedRef.current = false;
            fetchAgainRef.current = false;
        };
    }, []);

    // --- Fetch logic (stable reference — no deps that change) ---
    const fetchVeList = useCallback(async () => {
        if (fetchingRef.current) {
            fetchAgainRef.current = true;
            return;
        }
        fetchingRef.current = true;
        const fetchEpoch = pollingEpochRef.current;
        try {
            let fn = listFnRef.current;
            if (!fn) {
                const mod = await getWailsAppModule();
                fn = (mod as any).ListVirtualEmployees;
            }
            if (!fn) {
                hubReachableRef.current = false;
                if (mountedRef.current) setVersion(v => v + 1);
                return;
            }
            const rawList = await fn();
            if (!mountedRef.current || !pollingActiveRef.current || fetchEpoch !== pollingEpochRef.current) return;
            if (!Array.isArray(rawList)) {
                throw new Error("ListVirtualEmployees returned a non-array response");
            }
            const list: VirtualEmployeeEntry[] = rawList;
            const now = Date.now();
            setVeList(list);
            hubReachableRef.current = true;
            consecutiveFailuresRef.current = 0;
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
            if (!mountedRef.current || !pollingActiveRef.current || fetchEpoch !== pollingEpochRef.current) return;
            hubReachableRef.current = false;
            consecutiveFailuresRef.current += 1;
            setVersion(v => v + 1);
        } finally {
            fetchingRef.current = false;
            if (mountedRef.current && pollingActiveRef.current && fetchAgainRef.current) {
                fetchAgainRef.current = false;
                void fetchVeList();
            }
        }
    // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    const requestRefresh = useCallback(() => {
        if (eventThrottleRef.current !== undefined) {
            pendingEventRefreshRef.current = true;
            return;
        }
        skipNextPoll.current = true;
        void fetchVeList();
        eventThrottleRef.current = window.setTimeout(() => {
            eventThrottleRef.current = undefined;
            if (pendingEventRefreshRef.current) {
                pendingEventRefreshRef.current = false;
                skipNextPoll.current = true;
                void fetchVeList();
            }
        }, EVENT_REFRESH_THROTTLE_MS);
    }, [fetchVeList]);

    const applyStatusEvent = useCallback((data: any) => {
        const { ids, status } = veStatusEventInfo(data);
        if (ids.length === 0 || (status !== "online" && status !== "offline")) return;
        const now = Date.now();
        const info: VEPresenceInfo = { hubStatus: status, fetchedAt: now };
        let changed = false;
        for (const id of ids) {
            for (const key of participantIdentityKeys(id)) {
                presenceMapRef.current.set(key, info);
            }
        }
        setVeList(prev => prev.map((ve) => {
            const matches = ids.some((id) => participantIdentityMatches(ve.id, id) || participantIdentityMatches(ve.machine_id, id));
            if (!matches) return ve;
            changed = true;
            return { ...ve, online_status: status };
        }));
        if (changed) {
            hubReachableRef.current = true;
            setVersion(v => v + 1);
        }
    }, []);

    // --- Polling scheduler (single effect, stable deps) ---
    useEffect(() => {
        pollingActiveRef.current = hubConfigured;
        pollingEpochRef.current += 1;
        if (!hubConfigured) {
            fetchAgainRef.current = false;
            pendingEventRefreshRef.current = false;
            skipNextPoll.current = false;
            presenceMapRef.current = new Map();
            hubReachableRef.current = false;
            setVeList([]);
            setLastFetchAt(0);
            setVersion(v => v + 1);
            return;
        }

        let cancelled = false;

        const scheduleNext = () => {
            if (cancelled) return;
            const failureMultiplier = Math.max(1, 2 ** Math.min(consecutiveFailuresRef.current, 3));
            const interval = Math.min(POLL_INTERVAL_MAX, POLL_INTERVAL_NORMAL * failureMultiplier);
            const jitter = Math.floor(Math.random() * POLL_JITTER_MS);
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
            }, interval + jitter);
        };

        // Initial fetch
        fetchVeList().finally(() => { if (!cancelled) scheduleNext(); });

        return () => {
            cancelled = true;
            pollingActiveRef.current = false;
            fetchAgainRef.current = false;
            pendingEventRefreshRef.current = false;
            skipNextPoll.current = false;
            if (pollTimerRef.current !== undefined) {
                window.clearTimeout(pollTimerRef.current);
                pollTimerRef.current = undefined;
            }
            if (eventThrottleRef.current !== undefined) {
                window.clearTimeout(eventThrottleRef.current);
                eventThrottleRef.current = undefined;
            }
        };
    }, [hubConfigured, fetchVeList]);

    // --- WebSocket event listeners ---
    useEffect(() => {
        if (!hubConfigured) return;

        const handleStatusEvent = (data: any) => {
            applyStatusEvent(data);
            requestRefresh();
        };
        const handleListEvent = () => {
            requestRefresh();
        };

        const unsub1 = EventsOn("ve:status_change", handleStatusEvent);
        const unsub2 = EventsOn("ve:list_update", handleListEvent);

        return () => {
            if (typeof unsub1 === "function") unsub1(); else EventsOff("ve:status_change");
            if (typeof unsub2 === "function") unsub2(); else EventsOff("ve:list_update");
        };
    }, [hubConfigured, applyStatusEvent, requestRefresh]);

    // --- Visibility API: fetch immediately when returning to foreground ---
    useEffect(() => {
        if (!hubConfigured) return;

        const handler = () => {
            if (!document.hidden) {
                requestRefresh();
            }
        };
        document.addEventListener("visibilitychange", handler);
        return () => document.removeEventListener("visibilitychange", handler);
    }, [hubConfigured, requestRefresh]);

    // --- Tick for expiry re-evaluation ---
    useEffect(() => {
        const id = window.setInterval(() => setVersion(v => v + 1), TICK_INTERVAL);
        return () => window.clearInterval(id);
    }, []);

    // --- Status computation (reads from ref — stable identity) ---
    const getStatus = useCallback((veId: string): VEOnlineStatus => {
        if (!hubReachableRef.current) return "unknown";
        const matchedInfo = participantIdentityKeys(veId)
            .map((id) => presenceMapRef.current.get(id))
            .find(Boolean);
        if (!matchedInfo) return "unknown";
        if (Date.now() - matchedInfo.fetchedAt > STALE_THRESHOLD) return "unknown";
        return matchedInfo.hubStatus;
    }, []);

    const isStale = !hubReachableRef.current || (lastFetchAt > 0 && Date.now() - lastFetchAt > STALE_THRESHOLD);

    return { veList, getStatus, isStale, lastFetchAt, version };
}
