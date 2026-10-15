import { useCallback, useState } from "react";
import {
    findNewestHistoryAttachments,
    historyAttachmentsFromSources,
    type PromptHistoryAttachment,
} from "./promptHistory";

export interface ComposerAttachmentSnapshot {
    pending: PromptHistoryAttachment[];
    selectedFilePaths: string[];
}

interface AssistantInputHistoryOptions {
    applyInputValue: (nextValue: string) => void;
    /** Replace the composer's attached files when a history entry is recalled. */
    applyInputAttachments?: (next: ComposerAttachmentSnapshot) => void;
    /** Read the files currently attached to the composer, including unsaved edits. */
    readInputAttachments?: () => ComposerAttachmentSnapshot;
    inputRef: React.MutableRefObject<HTMLTextAreaElement | null>;
    inputValue: string;
    submittedPrompts: string[];
    /** Parallel to submittedPrompts. Missing rows mean that entry had no files. */
    submittedPromptAttachments?: PromptHistoryAttachment[][];
}

function emptySnapshot(): ComposerAttachmentSnapshot {
    return { pending: [], selectedFilePaths: [] };
}

function cloneSnapshot(snapshot: ComposerAttachmentSnapshot | null | undefined): ComposerAttachmentSnapshot {
    if (!snapshot) return emptySnapshot();
    return {
        pending: historyAttachmentsFromSources(snapshot.pending, []),
        selectedFilePaths: (snapshot.selectedFilePaths || []).map(path => path.trim()).filter(Boolean),
    };
}

function attachmentsForIndex(
    index: number,
    edits: Record<number, PromptHistoryAttachment[]>,
    saved: PromptHistoryAttachment[][] | undefined,
): PromptHistoryAttachment[] {
    if (Object.prototype.hasOwnProperty.call(edits, index)) return edits[index];
    return historyAttachmentsFromSources(saved?.[index] || [], []);
}

export function useAssistantInputHistory({
    applyInputValue,
    applyInputAttachments,
    readInputAttachments,
    inputRef,
    inputValue,
    submittedPrompts,
    submittedPromptAttachments,
}: AssistantInputHistoryOptions) {
    const [historyIndex, setHistoryIndex] = useState(-1);
    const [draftBeforeHistory, setDraftBeforeHistory] = useState<string | null>(null);
    const [draftAttachments, setDraftAttachments] = useState<ComposerAttachmentSnapshot | null>(null);
    const [historyEdits, setHistoryEdits] = useState<Record<number, string>>({});
    const [historyAttachmentEdits, setHistoryAttachmentEdits] = useState<Record<number, PromptHistoryAttachment[]>>({});

    const rememberHistoryEdit = useCallback((nextValue: string) => {
        if (historyIndex < 0) return;
        setHistoryEdits(prev => ({ ...prev, [historyIndex]: nextValue }));
    }, [historyIndex]);

    const resetHistoryBrowsing = useCallback(() => {
        setHistoryIndex(-1);
        setDraftBeforeHistory(null);
        setDraftAttachments(null);
        setHistoryEdits({});
        setHistoryAttachmentEdits({});
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

    const showHistoryAttachments = useCallback((pending: PromptHistoryAttachment[]) => {
        applyInputAttachments?.({ pending, selectedFilePaths: [] });
    }, [applyInputAttachments]);

    const recallHistory = useCallback((direction: 'up' | 'down') => {
        if (submittedPrompts.length === 0) return false;

        const currentEdits = historyEdits;
        const currentAttachmentEdits = historyAttachmentEdits;
        const currentHistoryIndex = historyIndex;
        const currentInputValue = inputRef.current?.value ?? inputValue;
        const currentAttachments = readInputAttachments ? cloneSnapshot(readInputAttachments()) : null;

        const rememberCurrentEntry = () => {
            if (currentHistoryIndex < 0) return;
            setHistoryEdits(prev => ({ ...prev, [currentHistoryIndex]: currentInputValue }));
            if (!currentAttachments) return;
            const flattened = historyAttachmentsFromSources(currentAttachments.pending, currentAttachments.selectedFilePaths);
            setHistoryAttachmentEdits(prev => ({ ...prev, [currentHistoryIndex]: flattened }));
        };

        const applyIndex = (index: number) => {
            applyInputValue(currentEdits[index] ?? submittedPrompts[index] ?? "");
            if (!applyInputAttachments) return;
            showHistoryAttachments(attachmentsForIndex(index, currentAttachmentEdits, submittedPromptAttachments));
        };

        if (direction === 'up') {
            if (currentHistoryIndex >= 0) {
                rememberCurrentEntry();
            } else {
                setDraftBeforeHistory(currentInputValue);
                setDraftAttachments(currentAttachments ?? emptySnapshot());
            }
            const nextIndex = currentHistoryIndex < 0 ? submittedPrompts.length - 1 : Math.max(0, currentHistoryIndex - 1);
            setHistoryIndex(nextIndex);
            applyIndex(nextIndex);
            return true;
        }

        if (currentHistoryIndex < 0) return false;
        rememberCurrentEntry();
        if (currentHistoryIndex >= submittedPrompts.length - 1) {
            setHistoryIndex(-1);
            applyInputValue(draftBeforeHistory ?? "");
            applyInputAttachments?.(draftAttachments ?? emptySnapshot());
            return true;
        }
        const nextIndex = currentHistoryIndex + 1;
        setHistoryIndex(nextIndex);
        applyIndex(nextIndex);
        return true;
    }, [applyInputAttachments, applyInputValue, draftAttachments, draftBeforeHistory, historyAttachmentEdits, historyEdits, historyIndex, inputRef, inputValue, readInputAttachments, showHistoryAttachments, submittedPromptAttachments, submittedPrompts]);

    const exitHistoryBrowsing = useCallback(() => {
        if (historyIndex < 0) return false;
        setHistoryIndex(-1);
        setHistoryEdits({});
        setHistoryAttachmentEdits({});
        applyInputValue(draftBeforeHistory ?? "");
        applyInputAttachments?.(draftAttachments ?? emptySnapshot());
        setDraftBeforeHistory(null);
        setDraftAttachments(null);
        return true;
    }, [applyInputAttachments, applyInputValue, draftAttachments, draftBeforeHistory, historyIndex]);

    const acceptHistoryText = useCallback((text: string) => {
        if (!applyInputAttachments) return;
        const pending = findNewestHistoryAttachments(submittedPrompts, submittedPromptAttachments, text);
        if (!pending) return;
        showHistoryAttachments(pending);
        if (historyIndex >= 0) {
            setHistoryAttachmentEdits(prev => ({ ...prev, [historyIndex]: pending }));
        }
    }, [applyInputAttachments, historyIndex, showHistoryAttachments, submittedPromptAttachments, submittedPrompts]);

    return { acceptHistoryText, exitHistoryBrowsing, isSelectionCollapsedAtBoundary, recallHistory, rememberHistoryEdit, resetHistoryBrowsing };
}
