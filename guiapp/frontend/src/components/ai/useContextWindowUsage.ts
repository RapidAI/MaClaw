import { useCallback, useEffect, useState } from "react";
import { EventsOn } from "../../../wailsjs/runtime";
import { normalizeAssistantSessionKey } from "./aiAssistantPanelSessionUtils";
import { parseWailsEventObject } from "./codingTaskMode";

export type ContextWindowUsage = { used: number; max: number; estimated?: boolean };

/** Clear one session's live sample. Status fallback can then show the reset state. */
export function removeContextWindowUsageForSession(
    usageBySession: Record<string, ContextWindowUsage>,
    sessionKey: string,
): Record<string, ContextWindowUsage> {
    const key = normalizeAssistantSessionKey(sessionKey);
    if (!key || !(key in usageBySession)) return usageBySession;
    const next = { ...usageBySession };
    delete next[key];
    return next;
}

export function useContextWindowUsage(): {
    usageBySession: Record<string, ContextWindowUsage>;
    resetSession: (sessionKey: string) => void;
} {
    const [usageBySession, setUsageBySession] = useState<Record<string, ContextWindowUsage>>({});
    useEffect(() => {
        const off = EventsOn("ai-assistant-context-usage", (payload: unknown) => {
            const event = parseWailsEventObject(payload);
            const sessionKey = normalizeAssistantSessionKey(String(event.session_key || ""));
            const max = Number(event.context_max_tokens) || 0;
            if (!sessionKey || max <= 0) return;
            const used = Math.max(0, Number(event.context_used_tokens) || 0);
            const estimated = event.context_usage_estimated === true;
            setUsageBySession(prev => ({ ...prev, [sessionKey]: { used, max, estimated } }));
        });
        return () => { if (typeof off === "function") off(); };
    }, []);
    const resetSession = useCallback((sessionKey: string) => {
        setUsageBySession(prev => removeContextWindowUsageForSession(prev, sessionKey));
    }, []);
    return { usageBySession, resetSession };
}
