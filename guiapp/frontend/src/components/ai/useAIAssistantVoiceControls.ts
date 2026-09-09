import { useCallback, useEffect, useRef } from "react";
import type { UseVoiceInputResult } from "./useVoiceInput";

interface VoiceControlsOptions {
    inputRef: React.MutableRefObject<HTMLTextAreaElement | null>;
    petFocusInputSeq: number;
    petVoiceStartSeq: number;
    ready: boolean;
    voiceInput: UseVoiceInputResult;
}

export function useAIAssistantVoiceControls({ inputRef, petFocusInputSeq, petVoiceStartSeq, ready, voiceInput }: VoiceControlsOptions) {
    const voiceHoldTimerRef = useRef<number | null>(null);
    const voiceHoldActiveRef = useRef(false);
    const voiceSuppressClickRef = useRef(false);

    useEffect(() => {
        if (!petVoiceStartSeq || !ready || voiceInput.state !== 'idle') return;
        void voiceInput.toggle();
    }, [petVoiceStartSeq, ready, voiceInput]);

    useEffect(() => {
        if (!petFocusInputSeq) return;
        inputRef.current?.focus();
    }, [inputRef, petFocusInputSeq]);

    const handleVoiceClick = useCallback(() => {
        if (voiceSuppressClickRef.current) {
            voiceSuppressClickRef.current = false;
            return;
        }
        if (!ready || voiceInput.state === 'transcribing') return;
        void voiceInput.toggle();
    }, [ready, voiceInput]);

    const finishVoicePointer = useCallback(() => {
        if (voiceHoldTimerRef.current) {
            clearTimeout(voiceHoldTimerRef.current);
            voiceHoldTimerRef.current = null;
        }
        if (voiceHoldActiveRef.current) {
            voiceHoldActiveRef.current = false;
            voiceSuppressClickRef.current = true;
            voiceInput.stopHold();
            window.setTimeout(() => { voiceSuppressClickRef.current = false; }, 250);
        }
    }, [voiceInput]);

    const handleVoicePointerDown = useCallback((event: React.PointerEvent<HTMLButtonElement>) => {
        if (event.button !== 0 || !ready || !voiceInput.asrReady || voiceInput.state !== "idle") return;
        voiceHoldTimerRef.current = window.setTimeout(() => {
            voiceHoldTimerRef.current = null;
            voiceHoldActiveRef.current = true;
            voiceSuppressClickRef.current = true;
            try { event.currentTarget.setPointerCapture(event.pointerId); } catch {}
            void voiceInput.startHold();
        }, 180);
    }, [ready, voiceInput]);

    const handleVoicePointerLeave = useCallback(() => {
        if (!voiceHoldActiveRef.current && voiceHoldTimerRef.current) {
            clearTimeout(voiceHoldTimerRef.current);
            voiceHoldTimerRef.current = null;
        }
    }, []);

    return { finishVoicePointer, handleVoiceClick, handleVoicePointerDown, handleVoicePointerLeave };
}
