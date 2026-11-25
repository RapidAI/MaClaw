import { useCallback, type ClipboardEvent, type DragEvent, type MutableRefObject } from "react";
import { remoteAssistantActive } from "../layout/remoteAssistantSession";

type ComposerFileGuards = {
    recordingActive: boolean;
    cloudShareReadOnlyRef: MutableRefObject<boolean>;
    blockComposerFilesRef: MutableRefObject<boolean>;
    wizardCoverActiveRef: MutableRefObject<boolean>;
    handlePasteBase: (event: ClipboardEvent<HTMLTextAreaElement>) => void;
    handleDragOverBase: (event: DragEvent<HTMLElement>) => void;
    handleDropBase: (event: DragEvent<HTMLElement>) => void;
};

/**
 * Block paste/drop attachments while the mic is live (composer hard-lock alone
 * is not enough) or while a remote/cloud session covers the panel. The
 * new-task wizard cover over a remote view collects attachments for the NEW
 * task, so the remote-session guard yields while it is up.
 */
export function useComposerClipboardGuards({ recordingActive, cloudShareReadOnlyRef, blockComposerFilesRef, wizardCoverActiveRef, handlePasteBase, handleDragOverBase, handleDropBase }: ComposerFileGuards) {
    const composerFilesBlocked = useCallback(() => recordingActive || cloudShareReadOnlyRef.current || blockComposerFilesRef.current
        || (remoteAssistantActive() && !wizardCoverActiveRef.current), [recordingActive]);
    const handlePaste = useCallback((event: ClipboardEvent<HTMLTextAreaElement>) => {
        if (composerFilesBlocked()) {
            event.preventDefault();
            return;
        }
        handlePasteBase(event);
    }, [composerFilesBlocked, handlePasteBase, recordingActive]);
    const handleDragOver = useCallback((event: DragEvent<HTMLElement>) => {
        if (composerFilesBlocked()) {
            event.preventDefault();
            event.dataTransfer.dropEffect = "none";
            return;
        }
        handleDragOverBase(event);
    }, [composerFilesBlocked, handleDragOverBase, recordingActive]);
    const handleDrop = useCallback((event: DragEvent<HTMLElement>) => {
        if (composerFilesBlocked()) {
            event.preventDefault();
            return;
        }
        handleDropBase(event);
    }, [composerFilesBlocked, handleDropBase, recordingActive]);
    return { handlePaste, handleDragOver, handleDrop };
}
