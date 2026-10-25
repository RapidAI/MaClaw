import { useEffect, useRef, type KeyboardEvent as ReactKeyboardEvent, type RefObject } from "react";

const FOCUSABLE = 'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [href]';

/**
 * Keeps keyboard focus inside an open modal and hands it back on close.
 *
 * Two things every modal in this app needs and neither dialog had:
 * - focus moves into the dialog when it opens, so Tab starts inside it rather
 *   than walking the page behind the backdrop;
 * - focus that lands outside (pointer, assistive tech, a stray tabindex) is
 *   pulled back in, and Tab/Shift+Tab wrap at the ends instead of escaping.
 *
 * Enter is left to the dialog: each one decides whether it submits.
 */
export function useModalFocusTrap(
    dialogRef: RefObject<HTMLElement | null>,
    options: { onEscape: () => void; initialFocusRef?: RefObject<HTMLElement | null> } ,
) {
    const previousFocusRef = useRef<HTMLElement | null>(null);
    const { onEscape, initialFocusRef } = options;

    useEffect(() => {
        previousFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
        // The dialog itself takes focus when nothing more specific is asked for:
        // its tabIndex={-1} makes it a valid target, and the first Tab then
        // enters the content in document order.
        const initial = initialFocusRef?.current ?? dialogRef.current;
        initial?.focus({ preventScroll: true });
        return () => {
            const previousFocus = previousFocusRef.current;
            previousFocusRef.current = null;
            if (previousFocus?.isConnected) previousFocus.focus({ preventScroll: true });
        };
        // Mount-only on purpose: the trap belongs to the dialog's lifetime, not
        // to whichever ref the caller happened to pass on a later render.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    // Focus can leave the dialog without a key press (pointer, screen reader).
    useEffect(() => {
        const handleFocusIn = (event: FocusEvent) => {
            const dialog = dialogRef.current;
            if (!dialog) return;
            if (event.target instanceof Node && !dialog.contains(event.target)) {
                dialog.focus({ preventScroll: true });
            }
        };
        document.addEventListener("focusin", handleFocusIn, true);
        return () => document.removeEventListener("focusin", handleFocusIn, true);
    }, [dialogRef]);

    // Focus can leave the dialog without a key press (pointer, screen reader),
    // and it can also be dropped on <body> when the focused control is removed
    // from the tree — body never fires focusin, so the handler above misses it.
    // The document-level listener covers both: Escape and Tab keep working even
    // when the dialog no longer holds focus.
    useEffect(() => {
        const handleKeyDown = (event: KeyboardEvent) => {
            const dialog = dialogRef.current;
            if (!dialog) return;
            const focusInside = dialog.contains(document.activeElement);
            if (focusInside) return; // The React handler owns the in-dialog case.
            if (event.key !== "Escape" && event.key !== "Tab") return;
            event.preventDefault();
            event.stopPropagation();
            if (event.key === "Escape") {
                onEscape();
                return;
            }
            const items = Array.from(dialog.querySelectorAll<HTMLElement>(FOCUSABLE));
            const target = event.shiftKey ? items[items.length - 1] : items[0];
            (target ?? dialog).focus({ preventScroll: true });
        };
        document.addEventListener("keydown", handleKeyDown, true);
        return () => document.removeEventListener("keydown", handleKeyDown, true);
    }, [dialogRef, onEscape]);

    const onKeyDown = (event: ReactKeyboardEvent) => {
        if (event.key === "Escape") {
            event.preventDefault();
            event.stopPropagation();
            onEscape();
            return;
        }
        if (event.key !== "Tab") return;
        const dialog = dialogRef.current;
        if (!dialog) return;
        const items = Array.from(dialog.querySelectorAll<HTMLElement>(FOCUSABLE));
        if (items.length === 0) return;
        const currentIndex = items.indexOf(document.activeElement as HTMLElement);
        // A focused dialog container (index -1) sends Tab to the first item and
        // Shift+Tab to the last, so the wrap is symmetric from the entry point.
        const nextIndex = event.shiftKey
            ? (currentIndex <= 0 ? items.length - 1 : currentIndex - 1)
            : (currentIndex === items.length - 1 || currentIndex === -1 ? 0 : currentIndex + 1);
        event.preventDefault();
        items[nextIndex].focus();
    };

    return { onKeyDown };
}
