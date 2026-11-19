import { useCallback, useRef, useState, type ChangeEvent, type CompositionEvent } from "react";

type ComposerTarget = HTMLTextAreaElement | HTMLInputElement;

export interface BufferedComposerValue {
    /** Value to render: the composition mirror while the IME composes, else upstream. */
    value: string;
    /** True between compositionstart and the commit flush. */
    isComposing: boolean;
    onChange: (event: ChangeEvent<ComposerTarget>) => void;
    onCompositionStart: (event: CompositionEvent<ComposerTarget>) => void;
    onCompositionEnd: (event: CompositionEvent<ComposerTarget>) => void;
}

/**
 * Buffers a controlled text field's value locally while an IME composition is
 * in progress. The rendered value still tracks the DOM — required, otherwise
 * React's controlled-input restore writes a stale value back and aborts the
 * composition — but the upstream `commit` only runs once, when the IME
 * commits, instead of per keystroke. That keeps heavy parent re-renders and
 * foreign value writes off the WebView2 IME path, where a re-render landing
 * mid-composition makes the IME commit raw encoding letters alongside the
 * hanzi (五笔编码与汉字混上屏).
 *
 * `commit` receives the field's latest value for non-composing inputs, for
 * the committed text at compositionend, and for a committed input when the
 * host dropped compositionend. While a composition is active the DOM wins:
 * external writes to `value` are masked until the commit flushes.
 */
export function useBufferedComposerValue(
    value: string,
    commit: (next: string) => void,
): BufferedComposerValue {
    const [compositionValue, setCompositionValue] = useState<string | null>(null);
    const composingRef = useRef(false);
    const commitRef = useRef(commit);
    commitRef.current = commit;

    const onChange = useCallback((event: ChangeEvent<ComposerTarget>) => {
        const inputComposing = (event.nativeEvent as InputEvent).isComposing;
        if (composingRef.current) {
            if (inputComposing !== false) {
                // Mirror the DOM so a controlled write can never differ from
                // it; `!== false` also accepts hosts that leave the flag
                // unset on composing inputs.
                setCompositionValue(event.target.value);
                return;
            }
            // The host dropped compositionend — this input is already
            // committed text, so fall through to the flush.
            composingRef.current = false;
        } else if (inputComposing) {
            // Seed guard for WebViews that deliver the first composing input
            // before compositionstart.
            composingRef.current = true;
            setCompositionValue(event.target.value);
            return;
        }
        setCompositionValue(null);
        commitRef.current(event.target.value);
    }, []);

    const onCompositionStart = useCallback((event: CompositionEvent<ComposerTarget>) => {
        composingRef.current = true;
        setCompositionValue(event.currentTarget.value);
    }, []);

    const onCompositionEnd = useCallback((event: CompositionEvent<ComposerTarget>) => {
        composingRef.current = false;
        setCompositionValue(null);
        // WebView2 may dispatch this before the final committed DOM and its
        // trailing non-composing input are visible; that late input then
        // flushes the same path once more with the real committed text.
        commitRef.current(event.currentTarget.value);
    }, []);

    return {
        value: compositionValue ?? value,
        isComposing: compositionValue !== null,
        onChange,
        onCompositionStart,
        onCompositionEnd,
    };
}
