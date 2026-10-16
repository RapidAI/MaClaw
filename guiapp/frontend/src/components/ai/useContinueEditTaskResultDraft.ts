import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from "react";
import { CONTINUE_EDIT_TASK_RESULT_EVENT, applyContinueEditTaskResultDraft, continueEditTaskResultPathFromEvent } from "./taskResultPreview";

interface ContinueEditTaskResultDraftOptions {
    lang: string;
    inputRef: RefObject<HTMLTextAreaElement | null>;
    updateInputValue: (nextValue: string) => void;
    resizeInput: () => void;
}

/** Focuses the composer and fills a continue-edit draft for a task-result file. */
export function useContinueEditTaskResultDraft({ lang, inputRef, updateInputValue, resizeInput }: ContinueEditTaskResultDraftOptions) {
    const pendingCaretRef = useRef<number | null>(null);
    const appliedSeqRef = useRef(0);
    const [focusSeq, setFocusSeq] = useState(0);

    useLayoutEffect(() => {
        if (focusSeq === 0 || appliedSeqRef.current === focusSeq) return;
        const caret = pendingCaretRef.current;
        if (caret == null) {
            appliedSeqRef.current = focusSeq;
            return;
        }
        const el = inputRef.current;
        if (!el) return;
        appliedSeqRef.current = focusSeq;
        el.focus();
        el.scrollIntoView({ block: "nearest" });
        el.style.height = "auto";
        el.style.height = `${el.scrollHeight}px`;
        const pos = Math.min(Math.max(caret, 0), el.value.length);
        el.selectionStart = pos;
        el.selectionEnd = pos;
        resizeInput();
        if (el.value.length < caret) {
            requestAnimationFrame(() => {
                const later = inputRef.current;
                if (!later) return;
                const nextPos = Math.min(caret, later.value.length);
                later.selectionStart = nextPos;
                later.selectionEnd = nextPos;
                resizeInput();
            });
        }
    }, [focusSeq, inputRef, resizeInput]);

    useEffect(() => {
        const onContinueEdit = (event: Event) => {
            const path = continueEditTaskResultPathFromEvent(event);
            if (!path) return;
            const current = String(inputRef.current?.value ?? "");
            const next = applyContinueEditTaskResultDraft(path, lang, current);
            pendingCaretRef.current = next.length;
            if (next !== current) updateInputValue(next);
            setFocusSeq((n) => n + 1);
        };
        window.addEventListener(CONTINUE_EDIT_TASK_RESULT_EVENT, onContinueEdit);
        return () => window.removeEventListener(CONTINUE_EDIT_TASK_RESULT_EVENT, onContinueEdit);
    }, [inputRef, lang, updateInputValue]);
}
