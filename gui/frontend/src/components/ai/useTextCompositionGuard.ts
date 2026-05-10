import { useRef } from "react";
import type { KeyboardEvent } from "react";

// Some WebViews emit compositionend just before the Enter keydown that commits the IME candidate.
const IME_COMMIT_ENTER_GRACE_MS = 50;

export function useTextCompositionGuard() {
    const activeRef = useRef(false);
    const commitEnterDeadlineRef = useRef(0);
    const commitEnterPendingRef = useRef(false);

    const consumePendingCommitEnter = () => {
        if (!commitEnterPendingRef.current) return false;
        if (Date.now() > commitEnterDeadlineRef.current) {
            commitEnterPendingRef.current = false;
            commitEnterDeadlineRef.current = 0;
            return false;
        }
        commitEnterPendingRef.current = false;
        commitEnterDeadlineRef.current = 0;
        return true;
    };

    return {
        onCompositionStart: () => {
            commitEnterDeadlineRef.current = 0;
            commitEnterPendingRef.current = false;
            activeRef.current = true;
        },
        onCompositionEnd: () => {
            activeRef.current = false;
            commitEnterPendingRef.current = true;
            commitEnterDeadlineRef.current = Date.now() + IME_COMMIT_ENTER_GRACE_MS;
        },
        shouldIgnoreKeyDown: (event: KeyboardEvent<HTMLTextAreaElement>) => {
            const nativeEvent = event.nativeEvent;
            const isIMEKey = nativeEvent.isComposing || nativeEvent.keyCode === 229 || event.key === "Process";
            if (isIMEKey && (event.key === "Enter" || event.key === "Process")) {
                consumePendingCommitEnter();
            }
            if (activeRef.current || isIMEKey) {
                return true;
            }
            return event.key === "Enter" && consumePendingCommitEnter();
        },
    };
}
