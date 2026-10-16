import { useCallback, useEffect, useRef, useState } from "react";

/** Keeps the redesigned workbench visible on first launch without deleting a
 * persisted local conversation. Any tab switch or explicit send dismisses it. */
export function useWorkbenchLandingMode(enabled: boolean, activeTabId: string, activeTabType: string) {
    const [requested, setRequested] = useState(enabled);
    const initialTabRef = useRef<string | null>(null);
    useEffect(() => {
        if (!enabled) return;
        if (initialTabRef.current === null) {
            initialTabRef.current = activeTabId;
            if (activeTabType !== "local") setRequested(false);
            return;
        }
        if (activeTabId !== initialTabRef.current) setRequested(false);
    }, [activeTabId, activeTabType, enabled]);
    const dismiss = useCallback(() => setRequested(false), []);
    return { requested, dismiss };
}

export function canShowWorkbenchLanding(input: {
    enabled: boolean; requested: boolean; local: boolean; coding: boolean;
    progress: number; thinking: boolean; processing: boolean; preparing: boolean;
    form: boolean; generating: boolean; review: boolean; starting: boolean; activeTask: boolean;
    queue: number; editing: boolean; interacted: boolean; workflowActive?: boolean;
}) {
    return input.enabled && input.requested && input.local && !input.coding && !input.activeTask
        && input.progress === 0 && !input.thinking && !input.processing
        && !input.preparing && !input.form && !input.generating && !input.review
        && !input.starting && !input.workflowActive && input.queue === 0 && !input.editing && !input.interacted;
}
