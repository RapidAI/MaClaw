import { useEffect, type MutableRefObject } from "react";

export function useAssistantExternalChatEvents(
    dispatchTaskIntentRef: MutableRefObject<((text: string, options?: Record<string, unknown>) => Promise<boolean>) | null>,
) {
    useEffect(() => {
        const handler = (e: Event) => {
            const detail = (e as CustomEvent).detail;
            if (typeof detail?.command === "string" && detail.command.trim()) {
                void dispatchTaskIntentRef.current?.(detail.command);
            }
        };
        window.addEventListener("ai-send-branch-command", handler);
        return () => window.removeEventListener("ai-send-branch-command", handler);
    }, [dispatchTaskIntentRef]);

    useEffect(() => {
        const handler = (e: Event) => {
            const text = (e as CustomEvent).detail?.text;
            if (typeof text === "string" && text.trim()) {
                e.preventDefault();
                void dispatchTaskIntentRef.current?.(text);
            }
        };
        window.addEventListener("maclaw:inject-chat-message", handler);
        return () => window.removeEventListener("maclaw:inject-chat-message", handler);
    }, [dispatchTaskIntentRef]);
}
