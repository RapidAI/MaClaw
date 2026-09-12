import { useState, useEffect, useCallback, useRef } from 'react';
import { GetHubUserRanking } from '../../../wailsjs/go/main/App';
import { EventsOn } from '../../../wailsjs/runtime';

export type HubRankingMedal = { rank: number; tokenRank: number; durationRank: number; totalUsers: number; rankChange?: number; trophyThreshold: number };

const HUB_RANKING_REFRESH_INTERVAL_MS = 30 * 60_000;
const HUB_RANKING_STARTUP_RETRY_DELAYS_MS = [30_000, 2 * 60_000, 8 * 60_000] as const;

export function useSidebarHubRanking(showRanking: boolean, trophyThreshold: number, activated: boolean): { medal: HubRankingMedal | null } {
    const [medal, setMedal] = useState<HubRankingMedal | null>(null);
    const rankingRequestSeqRef = useRef(0);
    const rankingLoadedRef = useRef(false);

    const fetchRanking = useCallback((): Promise<boolean> => {
        const requestSeq = ++rankingRequestSeqRef.current;
        if (!showRanking) {
            rankingLoadedRef.current = false;
            setMedal(null);
            return Promise.resolve(false);
        }
        return GetHubUserRanking()
            .then((result) => {
                if (requestSeq !== rankingRequestSeqRef.current) return rankingLoadedRef.current;
                const r = result as { token_rank?: number; duration_rank?: number; total_users?: number; rank_change?: number; error?: string } | null;
                if (!r || r.error) {
                    setMedal(null);
                    return false;
                }
                const tRank = r.token_rank || 0;
                const dRank = r.duration_rank || 0;
                // Pick the best (lowest non-zero) rank and show the badge for any valid Hub ranking response.
                let bestRank = 0;
                if (tRank > 0 && (dRank === 0 || tRank <= dRank)) { bestRank = tRank; }
                else if (dRank > 0) { bestRank = dRank; }
                rankingLoadedRef.current = true;
                setMedal({ rank: bestRank, tokenRank: tRank, durationRank: dRank, totalUsers: r.total_users || 0, rankChange: r.rank_change || 0, trophyThreshold });
                return true;
            })
            .catch(() => {
                if (requestSeq === rankingRequestSeqRef.current) setMedal(null);
                return false;
            });
    }, [showRanking, trophyThreshold]);
    const fetchRankingRef = useRef(fetchRanking);
    useEffect(() => { fetchRankingRef.current = fetchRanking; }, [fetchRanking]);

    useEffect(() => {
        if (!showRanking || !activated) return;
        const interval = window.setInterval(() => {
            fetchRankingRef.current();
        }, HUB_RANKING_REFRESH_INTERVAL_MS);
        return () => window.clearInterval(interval);
    }, [showRanking, activated]);

    useEffect(() => {
        if (!showRanking || !activated) {
            rankingLoadedRef.current = false;
            setMedal(null);
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
    }, [fetchRanking, showRanking, activated]);
    // Refresh ranking when token usage changes, throttled to avoid flooding Hub API.
    useEffect(() => {
        if (!showRanking) return;
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
    }, [showRanking]);

    return { medal };
}
