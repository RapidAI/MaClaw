import { useCallback, useRef, useState } from "react";
import { SelectAIAssistantFiles } from "../../../wailsjs/go/main/App";
import type { AttachmentInfo } from "./useBufferQueue";

/**
 * Draft for the new-task screen while another conversation stays on screen.
 * That screen is not the conversation composer, so its text and files live here.
 */
export function useNewTaskGuideDraft(setDraftInputValue?: (next: string) => void) {
    const guideInputRef = useRef<HTMLTextAreaElement | null>(null);
    const [guideAttachments, setGuideAttachments] = useState<AttachmentInfo[]>([]);
    const [guidePaths, setGuidePaths] = useState<string[]>([]);

    const updateGuideDraft = useCallback((next: string) => {
        setDraftInputValue?.(next);
    }, [setDraftInputValue]);

    const clearGuideDraft = useCallback(() => {
        setDraftInputValue?.("");
        if (guideInputRef.current) guideInputRef.current.value = "";
        setGuideAttachments([]);
        setGuidePaths([]);
    }, [setDraftInputValue]);

    const dropGuideFiles = useCallback(() => {
        setGuideAttachments(prev => (prev.length === 0 ? prev : []));
        setGuidePaths(prev => (prev.length === 0 ? prev : []));
    }, []);

    const browseGuideFile = useCallback(async () => {
        try {
            const selected = await SelectAIAssistantFiles();
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
    }, []);

    const removeGuideFile = useCallback((filePath: string) => {
        setGuidePaths(prev => prev.filter(path => path !== filePath));
    }, []);

    const clearGuideFiles = useCallback(() => {
        setGuideAttachments([]);
        setGuidePaths([]);
    }, []);

    return {
        guideInputRef,
        guideAttachments,
        guidePaths,
        setGuideAttachments,
        updateGuideDraft,
        clearGuideDraft,
        dropGuideFiles,
        browseGuideFile,
        removeGuideFile,
        clearGuideFiles,
    };
}
