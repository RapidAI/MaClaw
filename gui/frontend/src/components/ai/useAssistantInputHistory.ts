import { useCallback, useState } from "react";

interface AssistantInputHistoryOptions {
    applyInputValue: (nextValue: string) => void;
    inputRef: React.MutableRefObject<HTMLTextAreaElement | null>;
    inputValue: string;
    submittedPrompts: string[];
}

export function useAssistantInputHistory({ applyInputValue, inputRef, inputValue, submittedPrompts }: AssistantInputHistoryOptions) {
    const [historyIndex, setHistoryIndex] = useState(-1);
    const [draftBeforeHistory, setDraftBeforeHistory] = useState<string | null>(null);
    const [historyEdits, setHistoryEdits] = useState<Record<number, string>>({});

    const rememberHistoryEdit = useCallback((nextValue: string) => {
        if (historyIndex < 0) return;
        setHistoryEdits(prev => ({ ...prev, [historyIndex]: nextValue }));
    }, [historyIndex]);

    const resetHistoryBrowsing = useCallback(() => {
        setHistoryIndex(-1);
        setDraftBeforeHistory(null);
        setHistoryEdits({});
    }, []);

    const isSelectionCollapsedAtBoundary = useCallback((direction: 'up' | 'down') => {
        const input = inputRef.current;
        if (!input) return false;
        const { selectionStart, selectionEnd, value } = input;
        if (selectionStart !== selectionEnd) return false;
        if (selectionStart == null || selectionEnd == null) return false;
        if (direction === 'up') {
            return !value.slice(0, selectionStart).includes("\n");
        }
        return !value.slice(selectionEnd).includes("\n");
    }, [inputRef]);

    const recallHistory = useCallback((direction: 'up' | 'down') => {
        if (submittedPrompts.length === 0) return false;

        const currentEdits = historyEdits;
        const currentHistoryIndex = historyIndex;
        const currentInputValue = inputValue;

        const rememberCurrentEntry = () => {
            if (currentHistoryIndex < 0) return;
            setHistoryEdits(prev => ({ ...prev, [currentHistoryIndex]: currentInputValue }));
        };

        if (direction === 'up') {
            if (currentHistoryIndex >= 0) {
                rememberCurrentEntry();
            } else {
                setDraftBeforeHistory(currentInputValue);
            }
            const nextIndex = currentHistoryIndex < 0 ? submittedPrompts.length - 1 : Math.max(0, currentHistoryIndex - 1);
            setHistoryIndex(nextIndex);
            applyInputValue(currentEdits[nextIndex] ?? submittedPrompts[nextIndex]);
            return true;
        }

        if (currentHistoryIndex < 0) return false;
        rememberCurrentEntry();
        if (currentHistoryIndex >= submittedPrompts.length - 1) {
            setHistoryIndex(-1);
            applyInputValue(draftBeforeHistory ?? "");
            return true;
        }
        const nextIndex = currentHistoryIndex + 1;
        setHistoryIndex(nextIndex);
        applyInputValue(currentEdits[nextIndex] ?? submittedPrompts[nextIndex]);
        return true;
    }, [submittedPrompts, historyIndex, inputValue, draftBeforeHistory, historyEdits, applyInputValue]);

    const exitHistoryBrowsing = useCallback(() => {
        if (historyIndex < 0) return false;
        setHistoryIndex(-1);
        setHistoryEdits({});
        applyInputValue(draftBeforeHistory ?? "");
        setDraftBeforeHistory(null);
        return true;
    }, [historyIndex, draftBeforeHistory, applyInputValue]);

    return { exitHistoryBrowsing, isSelectionCollapsedAtBoundary, recallHistory, rememberHistoryEdit, resetHistoryBrowsing };
}
