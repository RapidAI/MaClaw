import { useEffect, useState } from "react";
import { EventsOn } from "../../../wailsjs/runtime";
import { normalizeAssistantSessionKey } from "./aiAssistantPanelSessionUtils";
import { parseWailsEventObject } from "./codingTaskMode";

export type ContextWindowUsage = { used: number; max: number };

export function useContextWindowUsage(): Record<string, ContextWindowUsage> {
    const [usageBySession, setUsageBySession] = useState<Record<string, ContextWindowUsage>>({});
    useEffect(() => {
        const off = EventsOn("ai-assistant-context-usage", (payload: unknown) => {
            const event = parseWailsEventObject(payload);
            const sessionKey = normalizeAssistantSessionKey(String(event.session_key || ""));
            const max = Number(event.context_max_tokens) || 0;
            if (!sessionKey || max <= 0) return;
            const used = Math.max(0, Number(event.context_used_tokens) || 0);
            setUsageBySession(prev => ({ ...prev, [sessionKey]: { used, max } }));
        });
        return () => { if (typeof off === "function") off(); };
    }, []);
    return usageBySession;
}
