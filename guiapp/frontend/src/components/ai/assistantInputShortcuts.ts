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

/** Ctrl/Cmd+Enter explicitly inserts a line break in controlled textareas. */
export function isLineBreakShortcut(event: Pick<KeyboardEvent, "key" | "altKey" | "ctrlKey" | "metaKey">): boolean {
    return event.key === "Enter" && (event.ctrlKey || event.metaKey) && !event.altKey;
}

/**
 * Insert a line break using the textarea's current DOM value. This avoids
 * dropping the newest keystrokes when React has not committed them yet.
 */
export function insertTextareaLineBreak(
    textarea: HTMLTextAreaElement,
    updateValue: (next: string) => void,
    afterUpdate?: () => void,
): string {
    const currentValue = textarea.value;
    const start = Math.min(textarea.selectionStart ?? currentValue.length, currentValue.length);
    const end = Math.max(start, Math.min(textarea.selectionEnd ?? start, currentValue.length));
    const next = `${currentValue.slice(0, start)}\n${currentValue.slice(end)}`;
    const caret = start + 1;
    updateValue(next);
    requestAnimationFrame(() => {
        afterUpdate?.();
        // The component might have unmounted or replaced this DOM node while
        // the controlled value was committing.
        if (!textarea.isConnected) return;
        textarea.focus({ preventScroll: true });
        textarea.setSelectionRange(caret, caret);
    });
    return next;
}
