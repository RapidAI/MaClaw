import { useCallback, useEffect, useRef, useState } from "react";
import { GetTTSEnabled, SetTTSEnabled } from "../../../wailsjs/go/main/App";
import { EventsOff, EventsOn } from "../../../wailsjs/runtime";
import { createLevelMeterRefs, startLevelMeter, stopLevelMeter, destroyLevelMeter } from "./ttsLevelMeter";

/**
 * Dispatch audio level as a DOM CustomEvent instead of React state.
 * TTSLevelBars listens to this event and updates bar heights via direct DOM
 * manipulation — zero React re-renders during playback.
 */
function emitTTSLevel(level: number): void {
    window.dispatchEvent(new CustomEvent("tts-level", { detail: level }));
}

/**
 * TTS readback hook with real-time audio level metering.
 *
 * Audio level updates bypass React state entirely — they are dispatched as
 * CustomEvents on `window` and consumed by TTSLevelBars via direct DOM ops.
 * Only `ttsEnabled` and `ttsPlaying` are React state (low-frequency toggles).
 */
export function useTTSReadback(audioOutputDeviceId?: string) {
    const [ttsEnabled, setTtsEnabledState] = useState(false);
    const [ttsPlaying, setTtsPlaying] = useState(false);
    const ttsEnabledRef = useRef(false);
    const ttsAudioQueueRef = useRef<string[]>([]);
    const ttsAudioPlayingRef = useRef(false);
    const ttsEnabledRecheckRef = useRef(false);
    const meterRefs = useRef(createLevelMeterRefs());

    useEffect(() => { ttsEnabledRef.current = ttsEnabled; }, [ttsEnabled]);
    useEffect(() => { GetTTSEnabled().then(v => setTtsEnabledState(!!v)).catch(() => {}); }, []);

    // PatchConfigFields emits config-changed/config-updated for every save, so
    // TTS toggles from other surfaces (bot voice tab, remote settings, tray)
    // refresh this flag without a remount — the panel stays mounted for the
    // app's lifetime, and a stale flag would drop or wrongly play audio.
    useEffect(() => {
        const reload = () => { void GetTTSEnabled().then(v => setTtsEnabledState(!!v)).catch(() => {}); };
        const offChanged = EventsOn('config-changed', reload);
        const offUpdated = EventsOn('config-updated', reload);
        return () => {
            if (typeof offChanged === 'function') offChanged();
            if (typeof offUpdated === 'function') offUpdated();
        };
    }, []);

    const setTtsEnabled = useCallback((next: boolean) => {
        setTtsEnabledState(next);
        void SetTTSEnabled(next).catch(() => {});
    }, []);

    const stopMetering = useCallback(() => {
        stopLevelMeter(meterRefs.current);
        emitTTSLevel(0);
    }, []);

    const playNextTTSAudio = useCallback(() => {
        if (ttsAudioPlayingRef.current) return;
        if (!ttsEnabledRef.current) {
            ttsAudioQueueRef.current = [];
            return;
        }
        const b64wav = ttsAudioQueueRef.current.shift();
        if (!b64wav) {
            setTtsPlaying(false);
            return;
        }
        ttsAudioPlayingRef.current = true;
        setTtsPlaying(true);

        const onEnded = () => {
            ttsAudioPlayingRef.current = false;
            playNextTTSAudio();
        };

        startLevelMeter(
            meterRefs.current,
            b64wav,
            audioOutputDeviceId,
            emitTTSLevel,
            onEnded,
        );
    }, [audioOutputDeviceId]);

    useEffect(() => {
        if (ttsEnabled) return;
        ttsAudioQueueRef.current = [];
        ttsAudioPlayingRef.current = false;
        setTtsPlaying(false);
        stopMetering();
    }, [ttsEnabled, stopMetering]);

    useEffect(() => {
        const handler = (b64wav: string) => {
            if (ttsEnabledRef.current) {
                ttsAudioQueueRef.current.push(b64wav);
                playNextTTSAudio();
                return;
            }
            // TTS can be switched on by another surface after this hook
            // mounted (bot voice tab, remote settings) — the assistant panel
            // stays mounted for the app's lifetime, so its once-loaded flag
            // would drop every event. Re-check the backend once before
            // dropping, and play the audio if the switch is now on.
            if (ttsEnabledRecheckRef.current) return;
            ttsEnabledRecheckRef.current = true;
            void GetTTSEnabled().then(on => {
                if (!on) return;
                ttsEnabledRef.current = true;
                setTtsEnabledState(true);
                ttsAudioQueueRef.current.push(b64wav);
                playNextTTSAudio();
            }).catch(() => {}).finally(() => { ttsEnabledRecheckRef.current = false; });
        };
        EventsOn("tts:audio", handler);
        return () => { EventsOff("tts:audio"); };
    }, [playNextTTSAudio]);

    // Cleanup AudioContext on unmount
    useEffect(() => {
        return () => { destroyLevelMeter(meterRefs.current); };
    }, []);

    return { ttsEnabled, setTtsEnabled, ttsPlaying };
}
