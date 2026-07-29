import type { KeyboardEvent } from "react";

/**
 * The assistant sends only on an unmodified Enter. Any modifier preserves the
 * textarea's native behavior, including Ctrl/Cmd+Enter for a line break.
 */
export function isPlainEnter(event: Pick<KeyboardEvent, "key" | "altKey" | "ctrlKey" | "metaKey" | "shiftKey">): boolean {
    return event.key === "Enter"
        && !event.shiftKey
        && !event.ctrlKey
        && !event.metaKey
        && !event.altKey;
}
