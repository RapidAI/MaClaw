import { useCallback, useRef, useState, type ClipboardEvent, type DragEvent } from "react";
import { SelectAIAssistantFiles } from "../../../wailsjs/go/main/App";
import { usePastedImageAttachments } from "./usePastedImageAttachments";

/**
 * Draft for the new-task screen while another conversation stays on screen.
 * Paste and drop stay on this draft so they do not attach to that conversation.
 */
const GUIDE_ATTACHMENT_SESSION = "new-task-guide";

export function useNewTaskGuideDraft(setDraftInputValue?: (next: string) => void, fileInputLock?: { current: boolean }) {
    const guideInputRef = useRef<HTMLTextAreaElement | null>(null);
    const {
        handlePaste: pasteGuideFiles,
        handleDragOver: dragOverGuideFiles,
        handleDrop: dropGuideFilesOnComposer,
        pendingAttachments: guideAttachments,
        setPendingAttachments: setGuideAttachments,
        settlePendingAttachments: settleGuideAttachments,
    } = usePastedImageAttachments(GUIDE_ATTACHMENT_SESSION);
    const handleGuidePaste = useCallback((event: ClipboardEvent<HTMLTextAreaElement>) => {
        if (fileInputLock?.current) {
            event.preventDefault();
            return;
        }
        pasteGuideFiles(event);
    }, [fileInputLock, pasteGuideFiles]);
    const handleGuideDragOver = useCallback((event: DragEvent<HTMLElement>) => {
        if (!fileInputLock?.current) {
            dragOverGuideFiles(event);
            return;
        }
        if (!Array.from(event.dataTransfer?.types || []).includes("Files")) return;
        event.preventDefault();
        event.dataTransfer.dropEffect = "none";
    }, [dragOverGuideFiles, fileInputLock]);
    const handleGuideDrop = useCallback((event: DragEvent<HTMLElement>) => {
        if (!fileInputLock?.current) {
            dropGuideFilesOnComposer(event);
            return;
        }
        if (!Array.from(event.dataTransfer?.types || []).includes("Files") && (event.dataTransfer?.files?.length || 0) === 0) return;
        event.preventDefault();
    }, [dropGuideFilesOnComposer, fileInputLock]);
    const [guidePaths, setGuidePaths] = useState<string[]>([]);

    const updateGuideDraft = useCallback((next: string) => {
        setDraftInputValue?.(next);
    }, [setDraftInputValue]);

    const clearGuideDraft = useCallback(() => {
        setDraftInputValue?.("");
        if (guideInputRef.current) guideInputRef.current.value = "";
        setGuideAttachments([]);
        setGuidePaths([]);
    }, [setDraftInputValue, setGuideAttachments]);

    const dropGuideFiles = useCallback(() => {
        // Always clear, even when the list is still empty, so an in-flight paste
        // cannot land after the cover has closed.
        setGuideAttachments([]);
        setGuidePaths(prev => (prev.length === 0 ? prev : []));
    }, [setGuideAttachments]);

    const browseGuideFile = useCallback(async () => {
        if (fileInputLock?.current) return;
        try {
            const selected = await SelectAIAssistantFiles();
            if (fileInputLock?.current) return;
            const valid = (selected || []).map(path => String(path || "").trim()).filter(Boolean);
            if (valid.length === 0) return;
            setGuidePaths(prev => {
                const seen = new Set(prev);
                const next = [...prev];
                for (const path of valid) {
                    if (seen.has(path)) continue;
                    seen.add(path);
                    next.push(path);
                }
                return next;
            });
        } catch (err) {
            console.error("Failed to select files:", err);
        }
    }, [fileInputLock]);

    const removeGuideFile = useCallback((filePath: string) => {
        setGuidePaths(prev => prev.filter(path => path !== filePath));
    }, []);

    const clearGuideFiles = useCallback(() => {
        setGuideAttachments([]);
        setGuidePaths([]);
    }, [setGuideAttachments]);

    return {
        guideInputRef,
        guideAttachments,
        guidePaths,
        setGuideAttachments,
        handleGuidePaste,
        handleGuideDragOver,
        handleGuideDrop,
        settleGuideAttachments,
        updateGuideDraft,
        clearGuideDraft,
        dropGuideFiles,
        browseGuideFile,
        removeGuideFile,
        clearGuideFiles,
    };
}
