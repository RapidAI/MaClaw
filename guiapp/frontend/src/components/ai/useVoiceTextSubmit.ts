import { useCallback } from "react";

import { normalizeASRText, shouldDispatchASRText } from "./asrTextUtils";
import { applyComposeActionToText, isBtwCommandText, isHistoryResetCommandText, normalizeInstallCommandText, type ComposeAction } from "./composeAction";
import type { AttachmentInfo } from "./useBufferQueue";
import type { VoiceInputSource } from "./useVoiceInput";

interface UseVoiceTextSubmitOptions {
    ready: boolean;
    recordingActive: boolean;
    composeAction: ComposeAction | null;
    setComposeAction: (action: ComposeAction | null) => void;
    codingTaskReadyForIntents: boolean;
    inputLocked: boolean;
    addEntry: (text: string, attachments: AttachmentInfo[], options?: { autoDrain?: boolean; steerWhenBusy?: boolean }) => unknown;
    clearComposerDraft: (options?: { clearAttachments?: boolean; focus?: boolean }) => void;
    dispatchBtwText: (commandText: string) => Promise<boolean>;
    /** Only its presence is consulted here; the raw /btw send stays with the caller. */
    sendBtwMessage?: unknown;
    sendMessageForTab: (text: string, options?: Record<string, unknown>) => Promise<boolean>;
    updateInputValue: (nextValue: string) => void;
    recordSubmittedPrompt?: (text: string) => void;
    refreshQueueInFlight: () => void;
    newConversationInFlightRef: { current: boolean };
    sendInFlightRef: { current: boolean };
}

/**
 * Voice/ASR turn submission. Kept out of AIAssistantPanel because it is a
 * self-contained dispatch policy: it only needs the send primitives plus two
 * in-flight guards, and inlining it pushed the panel past its line budget.
 */
export function useVoiceTextSubmit(options: UseVoiceTextSubmitOptions) {
    const {
        addEntry,
        clearComposerDraft,
        codingTaskReadyForIntents,
        composeAction,
        dispatchBtwText,
        inputLocked,
        newConversationInFlightRef,
        ready,
        recordSubmittedPrompt,
        recordingActive,
        refreshQueueInFlight,
        sendBtwMessage,
        sendInFlightRef,
        sendMessageForTab,
        setComposeAction,
        updateInputValue,
    } = options;
    return useCallback(async (text: string, _source?: VoiceInputSource) => {
        // Defense-in-depth: never send/queue empty or punctuation-only ASR noise.
        if (!ready || !shouldDispatchASRText(text)) return;
        const trimmed = normalizeASRText(text);
        // Honor active compose mode (goal / btw) so voice matches typed send semantics.
        const composed = applyComposeActionToText(trimmed, composeAction);
        // Live mic: never treat ASR as a session reset (would fight the recording card).
        if (!recordingActive && isHistoryResetCommandText(composed)) {
            if (newConversationInFlightRef.current) return;
            newConversationInFlightRef.current = true;
            setComposeAction(null);
            try {
                await sendMessageForTab(composed);
            } finally {
                newConversationInFlightRef.current = false;
            }
            return;
        }
        // Remote coding owns every turn, including /btw and install/control
        // commands. Preserve it in the tab queue until SSH is usable again.
        if (!codingTaskReadyForIntents) {
            addEntry(composed, [], { autoDrain: true, steerWhenBusy: false });
            setComposeAction(null);
            return;
        }
        if (isBtwCommandText(composed) && sendBtwMessage) {
            if (sendInFlightRef.current) return;
            sendInFlightRef.current = true;
            // Clear immediately — SendBtwQuery only resolves after the full side-query loop.
            clearComposerDraft({ clearAttachments: false });
            try {
                const ok = await dispatchBtwText(composed);
                if (!ok) {
                    // Restore draft so the user can retry after a hard failure.
                    updateInputValue(composed);
                    setComposeAction("btw");
                }
            } finally {
                sendInFlightRef.current = false;
            }
            return;
        }
        // Live mic: never queue voice as chat (would fight the recording session).
        if (recordingActive) return;
        // Install slash commands: same as typed send — always dispatch now (backend
        // handles them before the agent loop), even when the agent is busy.
        const voiceInstall = normalizeInstallCommandText(composed);
        if (voiceInstall) {
            if (sendInFlightRef.current) {
                addEntry(voiceInstall, [], { autoDrain: true });
                setComposeAction(null);
                return;
            }
            sendInFlightRef.current = true;
            setComposeAction(null);
            clearComposerDraft({ clearAttachments: false });
            try {
                const sent = await sendMessageForTab(voiceInstall);
                if (sent !== false) recordSubmittedPrompt?.(voiceInstall);
            } catch (err: unknown) {
                console.warn("[AIAssistantPanel] Voice install command send failed", err);
                updateInputValue(voiceInstall);
            } finally {
                sendInFlightRef.current = false;
                refreshQueueInFlight();
            }
            return;
        }
        // If agent is busy (inputLocked), queue the transcription for later delivery
        // instead of dropping it. The buffer queue auto-drains when the agent becomes idle.
        if (inputLocked) {
            addEntry(composed, [], { autoDrain: true });
            setComposeAction(null);
            return;
        }
        // A diarized recording can yield several chronological speaker turns.
        // The first turn starts a send; subsequent turns must be preserved for
        // the buffer queue instead of being silently dropped by the duplicate-
        // send guard. The queue drains after the active assistant turn ends.
        if (sendInFlightRef.current) {
            addEntry(composed, [], { autoDrain: true });
            setComposeAction(null);
            return;
        }
        sendInFlightRef.current = true;
        clearComposerDraft({ clearAttachments: false });
        try {
            const sent = await sendMessageForTab(composed);
            if (sent !== false) recordSubmittedPrompt?.(composed);
        } catch (err: unknown) {
            console.warn("[AIAssistantPanel] Voice prompt send failed", err);
        } finally {
            sendInFlightRef.current = false;
            // The send promise can settle before the session's busy state is
            // rendered. Wake the queue explicitly so a diarized follow-up
            // turn waits for this send, then drains promptly afterwards.
            refreshQueueInFlight();
        }
    }, [addEntry, clearComposerDraft, codingTaskReadyForIntents, composeAction, dispatchBtwText, inputLocked, ready, recordSubmittedPrompt, recordingActive, refreshQueueInFlight, sendBtwMessage, sendMessageForTab, updateInputValue]);
}
