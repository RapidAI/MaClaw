import { useEffect, useRef, type ChangeEvent, type KeyboardEvent, type PointerEvent } from "react";
import { GetChatFontSize, SetChatFontSize } from "../../../wailsjs/go/main/App";
import { chatFontSizeSaveQueue, claimSerialAttempt, clampChatFontSize, enqueueSerial, markSerialAttemptStarted } from "../ai/assistantFonts";

/** Unsaved size already shown by the slider. Config echoes must not roll it back. */
export type ChatFontSizeDraftRef = { current: number | null };

export { clampChatFontSize };

/** Same window as the UI-zoom slider in this panel. */
export const CHAT_FONT_SIZE_PERSIST_DEBOUNCE_MS = 280;

const RANGE_COMMIT_KEYS = new Set([
    "ArrowLeft",
    "ArrowRight",
    "ArrowUp",
    "ArrowDown",
    "Home",
    "End",
    "PageUp",
    "PageDown",
]);

/**
 * Chat font size is one control with two copies (UI settings and the API store page).
 * A range input does not commit on pointerup alone: keyboard changes and a drag that
 * ends outside the thumb never emit it, and writing inside onChange persists every
 * step. Match the zoom slider: debounce the input event, commit immediately on
 * pointerup and range keys, and flush a pending debounce when the control unmounts.
 *
 * lastWritten is the last size known to be on disk. It is set when SetChatFontSize
 * resolves and no newer snapshot has been applied since that save started, and when
 * the prop changes for a reason other than this control's own unsaved preview.
 * Copying the prop on every render would skip pointerup: onChange re-renders first.
 * Copying the mount value would also skip a later choice of that same size after
 * another writer changed the file. A repeat observation of that same size is not
 * another writer, including the second setup StrictMode runs on mount.
 * draftRef is that unsaved value for the config applier. It is set when the
 * committed size differs from the prop, and also when this control writes the
 * size already on screen before that write is known to be on disk. It clears
 * when that exact value settles. A no-op whose last write already matches does
 * not take a draft. Saves of this one field run in order; a size that was
 * replaced before it was sent is not sent. Each value that still needs a save
 * takes a queue attempt when it is shown. A matching snapshot may clear the
 * draft only after that attempt's save has started, so an older save of the
 * same size cannot cancel the one still waiting.
 */
export function useChatFontSizeCommit(
    chatFontSize: number,
    setChatFontSize: (value: number) => void,
    draftRef?: ChatFontSizeDraftRef,
) {
    const setSizeRef = useRef(setChatFontSize);
    setSizeRef.current = setChatFontSize;
    const lastWrittenRef = useRef<number | null>(null);
    const previewRef = useRef<number | null>(null);
    const seenSizeRef = useRef<number | null>(null);
    const pendingRef = useRef<number | null>(null);
    const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const writeAttemptRef = useRef(0);
    // Bumps when a snapshot, not this slider's preview, changes the size.
    // A save that started before that bump is no longer the size on disk.
    const diskEpochRef = useRef(0);

    const settleDraft = (value: number) => {
        if (draftRef && draftRef.current === value) draftRef.current = null;
    };

    const write = (value: number, attempt: number) => {
        pendingRef.current = null;
        if (chatFontSizeSaveQueue.attempt !== attempt) return;
        if (lastWrittenRef.current === value) {
            if (previewRef.current === value) previewRef.current = null;
            markSerialAttemptStarted(chatFontSizeSaveQueue, attempt);
            settleDraft(value);
            return;
        }
        const diskEpoch = diskEpochRef.current;
        const releaseDraft = () => {
            // A debounce of this same size has not been sent yet. It still owns the draft.
            if (pendingRef.current === value) return;
            settleDraft(value);
        };
        enqueueSerial(chatFontSizeSaveQueue, () => {
            // A newer commit is already queued. Sending this size could land
            // after that newer patch and put the old size back on disk.
            if (chatFontSizeSaveQueue.attempt !== attempt) return;
            markSerialAttemptStarted(chatFontSizeSaveQueue, attempt);
            return SetChatFontSize(value).then(() => {
                if (chatFontSizeSaveQueue.attempt !== attempt) return;
                if (diskEpochRef.current !== diskEpoch) {
                    releaseDraft();
                    return;
                }
                lastWrittenRef.current = value;
                if (previewRef.current === value) previewRef.current = null;
                settleDraft(value);
            }, () => {
                // The failed save does not emit a config snapshot, so nothing else
                // puts the stored size back. Re-read it when this attempt is still
                // the newest and a retry of the same size is not waiting. Hold the
                // draft until that read settles: clearing it first lets another
                // snapshot take the thumb, and the read then ignores disk because
                // the epoch moved. A failed re-read drops the draft and leaves the
                // thumb where the user put it.
                if (chatFontSizeSaveQueue.attempt !== attempt) return;
                if (pendingRef.current === value) return;
                return GetChatFontSize().then((s) => {
                    if (chatFontSizeSaveQueue.attempt !== attempt) return;
                    if (pendingRef.current != null) return;
                    if (diskEpochRef.current !== diskEpoch || typeof s !== "number" || s < 12) {
                        releaseDraft();
                        return;
                    }
                    const restored = clampChatFontSize(s);
                    lastWrittenRef.current = restored;
                    if (previewRef.current === value) previewRef.current = null;
                    releaseDraft();
                    if (seenSizeRef.current !== restored) setSizeRef.current(restored);
                }, () => {
                    if (chatFontSizeSaveQueue.attempt !== attempt) return;
                    if (pendingRef.current === value) return;
                    releaseDraft();
                });
            });
        });
    };

    useEffect(() => {
        const previous = seenSizeRef.current;
        seenSizeRef.current = chatFontSize;
        // The first observation records the size without treating it as saved.
        // A later run with that same size is not another writer: StrictMode
        // invokes this effect twice on mount, and copying that run into
        // lastWritten would skip a pointerup of the size already on screen.
        if (previous == null || previous === chatFontSize || previewRef.current === chatFontSize) return;
        lastWrittenRef.current = clampChatFontSize(chatFontSize);
        previewRef.current = null;
        diskEpochRef.current += 1;
    }, [chatFontSize]);

    useEffect(() => () => {
        if (timerRef.current == null) return;
        clearTimeout(timerRef.current);
        timerRef.current = null;
        const pending = pendingRef.current;
        if (pending != null) write(pending, writeAttemptRef.current);
    }, []);

    const commit = (raw: number, opts?: { debounce?: boolean }) => {
        const value = clampChatFontSize(raw);
        previewRef.current = value;
        // Also cover a write of the size already on screen. Until it settles, a
        // snapshot of a different size must not move the thumb. A no-op whose
        // last write already matches does not take a draft.
        if (draftRef && (value !== chatFontSize || lastWrittenRef.current !== value)) {
            draftRef.current = value;
        }
        setSizeRef.current(value);
        if (timerRef.current != null) {
            clearTimeout(timerRef.current);
            timerRef.current = null;
        }
        pendingRef.current = value;
        // Already on disk. Do not take an attempt: that would make the in-flight
        // save of a different size look stale while its request still lands.
        if (lastWrittenRef.current === value) {
            if (previewRef.current === value) previewRef.current = null;
            pendingRef.current = null;
            settleDraft(value);
            return;
        }
        // Claim before the debounce, so a matching snapshot during the wait
        // does not treat this size as already sent.
        const attempt = claimSerialAttempt(chatFontSizeSaveQueue);
        writeAttemptRef.current = attempt;
        if (opts?.debounce) {
            timerRef.current = setTimeout(() => {
                timerRef.current = null;
                write(value, attempt);
            }, CHAT_FONT_SIZE_PERSIST_DEBOUNCE_MS);
            return;
        }
        write(value, attempt);
    };

    const rangeHandlers = {
        onChange: (event: ChangeEvent<HTMLInputElement>) => {
            commit(Number(event.target.value), { debounce: true });
        },
        onPointerUp: (event: PointerEvent<HTMLInputElement>) => {
            commit(Number(event.currentTarget.value));
        },
        onKeyUp: (event: KeyboardEvent<HTMLInputElement>) => {
            if (!RANGE_COMMIT_KEYS.has(event.key)) return;
            commit(Number(event.currentTarget.value));
        },
    };

    return { commit, rangeHandlers };
}
