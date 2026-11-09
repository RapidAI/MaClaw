import { useCallback, useEffect, useRef } from "react";

const LOCAL_DRAFT_SYNC_DELAY_MS = 120;
const LOCAL_DRAFT_SYNC_MAX_WAIT_MS = 1000;

export function useAssistantLocalDraftSync(setDraftInputValue?: (text: string) => void) {
    const trailingTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const maxWaitTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const pendingValueRef = useRef<string | null>(null);
    const setDraftInputValueRef = useRef(setDraftInputValue);
    setDraftInputValueRef.current = setDraftInputValue;

    const clearTimers = useCallback(() => {
        if (trailingTimerRef.current) clearTimeout(trailingTimerRef.current);
        if (maxWaitTimerRef.current) clearTimeout(maxWaitTimerRef.current);
        trailingTimerRef.current = null;
        maxWaitTimerRef.current = null;
    }, []);

    const flushLocalDraft = useCallback(() => {
        clearTimers();
        if (pendingValueRef.current === null) return;
        const pendingValue = pendingValueRef.current;
        pendingValueRef.current = null;
        setDraftInputValueRef.current?.(pendingValue);
    }, [clearTimers]);

    useEffect(() => () => flushLocalDraft(), [flushLocalDraft]);

    const syncLocalDraft = useCallback((nextValue: string, deferParent: boolean) => {
        if (trailingTimerRef.current) clearTimeout(trailingTimerRef.current);
        trailingTimerRef.current = null;
        if (!deferParent) {
            clearTimers();
            pendingValueRef.current = null;
            setDraftInputValue?.(nextValue);
            return;
        }
        pendingValueRef.current = nextValue;
        if (!maxWaitTimerRef.current) {
            maxWaitTimerRef.current = setTimeout(flushLocalDraft, LOCAL_DRAFT_SYNC_MAX_WAIT_MS);
        }
        trailingTimerRef.current = setTimeout(flushLocalDraft, LOCAL_DRAFT_SYNC_DELAY_MS);
    }, [clearTimers, flushLocalDraft, setDraftInputValue]);

    return { flushLocalDraft, syncLocalDraft };
}
