import { useEffect, useRef } from "react";
import { remoteAssistantActive, remoteAssistantClose } from "../layout/remoteAssistantSession";

/**
 * The remote/cloud session covers whatever tab is active. ANY tab activation —
 * a sidebar task row, a freshly opened/resumed task, the switcher, a wizard
 * launch — must retire the cover, or the task the user asked for opens
 * invisibly underneath. A cloud→cloud handoff (the wizard creating the next
 * cloud task) publishes a new session without touching tabs, so it is
 * unaffected.
 */
export function useRemoteCoverRetireOnTabActivate(activeTabId: string) {
    const previousActiveTabIdRef = useRef(activeTabId);
    useEffect(() => {
        const previous = previousActiveTabIdRef.current;
        previousActiveTabIdRef.current = activeTabId;
        if (activeTabId === previous) return;
        if (remoteAssistantActive()) remoteAssistantClose();
    }, [activeTabId]);
}
