import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from "react";
import { CITE_MERMAID_DIAGRAM_EVENT, applyMermaidDiagramCitation, citeMermaidDiagramSourceFromEvent } from "./AssistantMermaidDiagram";

interface CiteMermaidDiagramDraftOptions {
    lang: string;
    inputRef: RefObject<HTMLTextAreaElement | null>;
    updateInputValue: (nextValue: string) => void;
    resizeInput: () => void;
}

/** Focuses the composer and quotes a diagram into the draft. */
export function useCiteMermaidDiagramDraft({ lang, inputRef, updateInputValue, resizeInput }: CiteMermaidDiagramDraftOptions) {
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
        const onCite = (event: Event) => {
            const source = citeMermaidDiagramSourceFromEvent(event);
            if (!source) return;
            const current = String(inputRef.current?.value ?? "");
            const next = applyMermaidDiagramCitation(source, lang, current);
            pendingCaretRef.current = next.length;
            if (next !== current) updateInputValue(next);
            setFocusSeq((n) => n + 1);
        };
        window.addEventListener(CITE_MERMAID_DIAGRAM_EVENT, onCite);
        return () => window.removeEventListener(CITE_MERMAID_DIAGRAM_EVENT, onCite);
    }, [inputRef, lang, updateInputValue]);
}
