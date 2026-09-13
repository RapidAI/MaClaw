import { useState, useEffect, useCallback, useRef } from 'react';
import { GetHubUserRanking } from '../../../wailsjs/go/main/App';
import { EventsOn } from '../../../wailsjs/runtime';

export type HubRankingSnapshot = { tokenRank: number; durationRank: number };

const HUB_RANKING_REFRESH_INTERVAL_MS = 30 * 60_000;
const HUB_RANKING_STARTUP_RETRY_DELAYS_MS = [30_000, 2 * 60_000, 8 * 60_000] as const;

export function systemRankingLabel(lang: string, snapshot: HubRankingSnapshot | null, fallback: string): string {
    const ranks = snapshot ? [snapshot.tokenRank, snapshot.durationRank].filter(rank => rank > 0) : [];
    if (ranks.length === 0) return fallback;
    const place = Math.min(...ranks);
    return lang === 'en' ? `#${place}` : `第${place}名`;
}

export function useSidebarHubRanking(enabled: boolean, activated: boolean): HubRankingSnapshot | null {
    const [snapshot, setSnapshot] = useState<HubRankingSnapshot | null>(null);
    const rankingRequestSeqRef = useRef(0);
    const rankingLoadedRef = useRef(false);

    const fetchRanking = useCallback((): Promise<boolean> => {
        const requestSeq = ++rankingRequestSeqRef.current;
        if (!enabled) {
            rankingLoadedRef.current = false;
            setSnapshot(null);
            return Promise.resolve(false);
        }
        return GetHubUserRanking()
            .then((result) => {
                if (requestSeq !== rankingRequestSeqRef.current) return rankingLoadedRef.current;
                const r = result as { token_rank?: number; duration_rank?: number; error?: string } | null;
                if (!r || r.error) {
                    if (!rankingLoadedRef.current) setSnapshot(null);
                    return rankingLoadedRef.current;
                }
                rankingLoadedRef.current = true;
                setSnapshot({
                    tokenRank: r.token_rank || 0,
                    durationRank: r.duration_rank || 0,
                });
                return true;
            })
            .catch(() => {
                if (requestSeq === rankingRequestSeqRef.current && !rankingLoadedRef.current) setSnapshot(null);
                return rankingLoadedRef.current;
            });
    }, [enabled]);
    const fetchRankingRef = useRef(fetchRanking);
    useEffect(() => { fetchRankingRef.current = fetchRanking; }, [fetchRanking]);

    useEffect(() => {
        if (!enabled || !activated) return;
        const interval = window.setInterval(() => {
            fetchRankingRef.current();
        }, HUB_RANKING_REFRESH_INTERVAL_MS);
        return () => window.clearInterval(interval);
    }, [enabled, activated]);

    useEffect(() => {
        if (!enabled || !activated) {
            rankingLoadedRef.current = false;
            setSnapshot(null);
            return;
        }
        let cancelled = false;
        const retryTimers: number[] = [];
        const attempt = (retryIndex: number) => {
            if (rankingLoadedRef.current) return;
            fetchRanking().then((loaded) => {
                if (cancelled || loaded || rankingLoadedRef.current || retryIndex >= HUB_RANKING_STARTUP_RETRY_DELAYS_MS.length) return;
                const timer = window.setTimeout(() => attempt(retryIndex + 1), HUB_RANKING_STARTUP_RETRY_DELAYS_MS[retryIndex]);
                retryTimers.push(timer);
            });
        };
        attempt(0);
        return () => {
            cancelled = true;
            retryTimers.forEach(timer => window.clearTimeout(timer));
        };
    }, [fetchRanking, enabled, activated]);

    useEffect(() => {
        if (!enabled || !activated) return;
        let throttleTimer: number | undefined;
        let pending = false;
        const onTokenUsageChanged = () => {
            if (throttleTimer !== undefined) {
                pending = true;
                return;
            }
            throttleTimer = window.setTimeout(() => {
                throttleTimer = undefined;
                fetchRankingRef.current();
                if (pending) {
                    pending = false;
                    throttleTimer = window.setTimeout(() => {
                        throttleTimer = undefined;
                        fetchRankingRef.current();
                    }, 60_000);
                }
            }, 5_000);
        };
        const unsubscribe = EventsOn("llm-token-usage-changed", onTokenUsageChanged);
        return () => {
            window.clearTimeout(throttleTimer);
            if (typeof unsubscribe === 'function') unsubscribe();
        };
    }, [enabled, activated]);

    return snapshot;
}
