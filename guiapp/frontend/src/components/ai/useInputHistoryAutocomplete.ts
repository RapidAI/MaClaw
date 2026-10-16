import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent, type RefObject } from "react";
import { isPlainEnter } from "./assistantInputShortcuts";
import { matchHistoryPrefix } from "./inputHistoryAutocompleteUtils";

const EMPTY_MATCHES: string[] = [];

interface UseInputHistoryAutocompleteOptions {
    inputValue: string;
    submittedPrompts: readonly string[];
    applyInputValue: (next: string) => void;
    inputRef: RefObject<HTMLTextAreaElement | null>;
    /** When true (e.g. IME composing), keep list closed. */
    disabled?: boolean;
}

export function useInputHistoryAutocomplete({
    inputValue,
    submittedPrompts,
    applyInputValue,
    inputRef,
    disabled = false,
}: UseInputHistoryAutocompleteOptions) {
    // Always compute raw matches so disable (IME / busy) only hides the popup
    // and does not wipe sticky Esc-dismiss or match identity.
    const rawMatches = useMemo(
        () => matchHistoryPrefix(inputValue, submittedPrompts),
        [inputValue, submittedPrompts],
    );
    const matches = disabled ? EMPTY_MATCHES : rawMatches;

    const [open, setOpen] = useState(false);
    const [selectedIndex, setSelectedIndexState] = useState(0);
    // After Esc/accept, stay closed until the draft text changes.
    const dismissedForInputRef = useRef<string | null>(null);
    const prevMatchKeyRef = useRef("");

    // Track match identity so we only reset selection when the suggestion set changes,
    // not on every parent re-render with an equivalent array.
    const matchKey = rawMatches.join("\u0001");

    useEffect(() => {
        if (disabled) {
            setOpen(false);
            // Keep dismissedForInputRef / selection identity across temporary disable.
            return;
        }
        if (rawMatches.length === 0) {
            setOpen(false);
            setSelectedIndexState(0);
            // Clear sticky dismiss so retyping the same prefix after clearing can reopen.
            dismissedForInputRef.current = null;
            prevMatchKeyRef.current = "";
            return;
        }
        if (dismissedForInputRef.current === inputValue) {
            setOpen(false);
            return;
        }
        dismissedForInputRef.current = null;
        setOpen(true);
        // Reset highlight only when the suggestion set identity changes (not when the
        // draft grows but still yields the same completions).
        if (prevMatchKeyRef.current !== matchKey) {
            prevMatchKeyRef.current = matchKey;
            setSelectedIndexState(0);
        }
        // eslint-disable-next-line react-hooks/exhaustive-deps -- matchKey captures rawMatches content
    }, [disabled, matchKey, rawMatches.length, inputValue]);

    const safeSelectedIndex = rawMatches.length === 0
        ? 0
        : Math.max(0, Math.min(selectedIndex, rawMatches.length - 1));

    const setSelectedIndex = useCallback((index: number) => {
        if (rawMatches.length === 0) {
            setSelectedIndexState(0);
            return;
        }
        setSelectedIndexState(Math.max(0, Math.min(index, rawMatches.length - 1)));
    }, [rawMatches.length]);

    const dismiss = useCallback(() => {
        dismissedForInputRef.current = inputValue;
        setOpen(false);
        setSelectedIndexState(0);
    }, [inputValue]);

    const accept = useCallback((index = safeSelectedIndex) => {
        if (disabled || !open || rawMatches.length === 0) return false;
        const pick = rawMatches[Math.max(0, Math.min(index, rawMatches.length - 1))];
        if (!pick) return false;
        applyInputValue(pick);
        // Sticky-close on the completed value so accepting a mid-length item
        // (e.g. "hello" while "hello world" exists) does not immediately reopen.
        dismissedForInputRef.current = pick;
        setOpen(false);
        setSelectedIndexState(0);
        requestAnimationFrame(() => {
            const el = inputRef.current;
            if (!el) return;
            el.focus();
            const end = pick.length;
            el.setSelectionRange(end, end);
        });
        return true;
    }, [applyInputValue, disabled, inputRef, open, rawMatches, safeSelectedIndex]);

    const moveSelection = useCallback((delta: number) => {
        if (disabled || !open || rawMatches.length === 0) return false;
        const len = rawMatches.length;
        setSelectedIndexState((prev) => {
            const clamped = Math.max(0, Math.min(prev, len - 1));
            const next = clamped + delta;
            if (next < 0) return len - 1;
            if (next >= len) return 0;
            return next;
        });
        return true;
    }, [disabled, open, rawMatches.length]);

    // Stable identities for keyboard + list clicks; always delegate to latest impls.
    const acceptRef = useRef(accept);
    acceptRef.current = accept;
    const moveSelectionRef = useRef(moveSelection);
    moveSelectionRef.current = moveSelection;
    const dismissRef = useRef(dismiss);
    dismissRef.current = dismiss;

    const acceptStable = useCallback((index?: number) => {
        return index === undefined ? acceptRef.current() : acceptRef.current(index);
    }, []);

    /**
     * Returns true when the key event was handled by autocomplete
     * (caller should preventDefault / skip send & history recall).
     */
    const handleKeyDown = useCallback((event: KeyboardEvent<HTMLTextAreaElement>): boolean => {
        if (disabled || !open || rawMatches.length === 0) return false;

        if (event.key === "ArrowDown") {
            event.preventDefault();
            moveSelectionRef.current(1);
            return true;
        }
        if (event.key === "ArrowUp") {
            event.preventDefault();
            moveSelectionRef.current(-1);
            return true;
        }
        if (event.key === "Escape") {
            event.preventDefault();
            dismissRef.current();
            return true;
        }
        // Plain Enter completes; modified Enter stays with the textarea.
        if (isPlainEnter(event)) {
            event.preventDefault();
            acceptRef.current();
            return true;
        }
        // Tab completes; Shift+Tab must fall through for reverse focus navigation.
        if (
            event.key === "Tab"
            && !event.shiftKey
            && !event.ctrlKey
            && !event.metaKey
            && !event.altKey
        ) {
            event.preventDefault();
            acceptRef.current();
            return true;
        }
        return false;
    }, [disabled, open, rawMatches.length]);

    const isOpen = !disabled && open && rawMatches.length > 0;

    return {
        open: isOpen,
        matches,
        selectedIndex: safeSelectedIndex,
        setSelectedIndex,
        accept: acceptStable,
        dismiss,
        handleKeyDown,
    };
}
