import {
    claimSerialAttempt,
    createSerialQueue,
    enqueueSerial,
    markSerialAttemptStarted,
    resetSerialQueue,
} from "./assistantFonts";

/**
 * One in-flight patch. Overlapping saves of this flag can commit in either
 * order, so a fast older click must not be written after the value now shown.
 */
export const chatBubblesSaveQueue = createSerialQueue();

export function resetChatBubblesSaveQueue() {
    resetSerialQueue(chatBubblesSaveQueue);
}

/**
 * A config snapshot applies only when it does not fight the value the switch
 * has already shown. A matching draft is the save echoing back. Any other
 * snapshot while a draft is set is older and is ignored. A missing key is not
 * "turn it back on": the stored default is on, and an omitted field must not
 * flip a switch the user just turned off.
 */
export function applyChatBubblesSnapshot(
    incoming: unknown,
    draft: boolean | null,
): { enabled: boolean; clearDraft: boolean } | null {
    if (typeof incoming !== "boolean") return null;
    if (draft != null && draft !== incoming) return null;
    return { enabled: incoming, clearDraft: draft === incoming };
}

/**
 * A config read is tagged with the switch epoch from the moment it was issued.
 * A choice or a newer echo moves that epoch. The read must not move the switch
 * after that, even once its draft has been cleared.
 * A live event payload is not a read: pass the current epoch for both sides.
 */
export function chatBubblesFromRead(
    incoming: unknown,
    draft: boolean | null,
    epochAtRead: number,
    epochNow: number,
): { enabled: boolean; clearDraft: boolean } | null {
    if (epochAtRead !== epochNow) return null;
    return applyChatBubblesSnapshot(incoming, draft);
}

/**
 * Drop the draft only for the echo of a save that has already been sent.
 * A matching snapshot that arrives before that send is still the previous value.
 */
export function chatBubblesEchoClearsDraft(enabled: boolean, draft: boolean | null, newestHasStarted: boolean): boolean {
    return draft === enabled && newestHasStarted;
}

/**
 * Show the choice immediately and persist it after any save already in flight.
 * A queued attempt whose value is no longer the one on screen does not write.
 * The draft stays until the config echo of the save that was actually sent.
 */
export function publishChatBubbles(
    enabled: boolean,
    draftRef: { current: boolean | null } | undefined,
    setEnabled: ((enabled: boolean) => void) | undefined,
    save: (enabled: boolean) => Promise<void>,
    readStored: () => Promise<boolean>,
): void {
    if (draftRef) draftRef.current = enabled;
    setEnabled?.(enabled);
    const queue = chatBubblesSaveQueue;
    const attempt = claimSerialAttempt(queue);
    enqueueSerial(queue, () => {
        if (queue.attempt !== attempt) return;
        markSerialAttemptStarted(queue, attempt);
        return save(enabled).then(() => {
            if (queue.attempt !== attempt) return;
        }, () => {
            if (queue.attempt !== attempt) return;
            return readStored().then((stored) => {
                if (queue.attempt !== attempt) return;
                const on = stored !== false;
                if (draftRef && draftRef.current === enabled) draftRef.current = null;
                if (on !== enabled) setEnabled?.(on);
            }, () => {
                if (queue.attempt !== attempt) return;
                if (draftRef?.current === enabled) draftRef.current = null;
            });
        });
    });
}
