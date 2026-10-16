import { useCallback, useEffect, useRef, useState } from "react";

export function useCloudTreeRefresh() {
    const [localWorkspaceRefreshToken, setLocalWorkspaceRefreshToken] = useState(0);
    const bumpLocalWorkspaceRefresh = useCallback(() => {
        setLocalWorkspaceRefreshToken((value) => value + 1);
    }, []);
    const cloudTreeRefreshTimerRef = useRef<number | null>(null);
    const cloudTreeRefreshTabIdRef = useRef<string | null>(null);
    const clearCloudTreeRefreshTimer = useCallback(() => {
        if (cloudTreeRefreshTimerRef.current == null) return;
        window.clearTimeout(cloudTreeRefreshTimerRef.current);
        cloudTreeRefreshTimerRef.current = null;
    }, []);
    const scheduleCloudTreeRefresh = useCallback(() => {
        clearCloudTreeRefreshTimer();
        const tabId = cloudTreeRefreshTabIdRef.current;
        cloudTreeRefreshTimerRef.current = window.setTimeout(() => {
            cloudTreeRefreshTimerRef.current = null;
            if (cloudTreeRefreshTabIdRef.current !== tabId) return;
            bumpLocalWorkspaceRefresh();
        }, 400);
    }, [bumpLocalWorkspaceRefresh, clearCloudTreeRefreshTimer]);
    useEffect(() => () => {
        clearCloudTreeRefreshTimer();
    }, [clearCloudTreeRefreshTimer]);
    return {
        localWorkspaceRefreshToken,
        bumpLocalWorkspaceRefresh,
        cloudTreeRefreshTabIdRef,
        scheduleCloudTreeRefresh,
        clearCloudTreeRefreshTimer,
    };
}
