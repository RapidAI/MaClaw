import { useCallback, useEffect, useRef, useState } from "react";
import { GetTTSEnabled, SetTTSEnabled } from "../../../wailsjs/go/main/App";
import { EventsOff, EventsOn } from "../../../wailsjs/runtime";
import { createLevelMeterRefs, startLevelMeter, stopLevelMeter, destroyLevelMeter } from "./ttsLevelMeter";

/**
 * TTS readback hook with real-time audio level metering.
 *
 * Plays audio entirely through Web Audio API (AudioBufferSourceNode → AnalyserNode → destination).
 * This guarantees the AnalyserNode receives audio data for level metering.
 * Audio device selection is handled via AudioContext.setSinkId (Chromium 110+).
 */
export function useTTSReadback(audioOutputDeviceId?: string) {
    const [ttsEnabled, setTtsEnabledState] = useState(false);
    const [ttsPlaying, setTtsPlaying] = useState(false);
    const [ttsAudioLevel, setTtsAudioLevel] = useState(0);
    const ttsEnabledRef = useRef(false);
    const ttsAudioQueueRef = useRef<string[]>([]);
    const ttsAudioPlayingRef = useRef(false);
    const meterRefs = useRef(createLevelMeterRefs());

    useEffect(() => { ttsEnabledRef.current = ttsEnabled; }, [ttsEnabled]);
    useEffect(() => { GetTTSEnabled().then(v => setTtsEnabledState(!!v)).catch(() => {}); }, []);

    const setTtsEnabled = useCallback((next: boolean) => {
        setTtsEnabledState(next);
        void SetTTSEnabled(next).catch(() => {});
    }, []);

    const stopMetering = useCallback(() => {
        stopLevelMeter(meterRefs.current);
        setTtsAudioLevel(0);
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
            setTtsAudioLevel,
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
            if (!ttsEnabledRef.current) return;
            ttsAudioQueueRef.current.push(b64wav);
            playNextTTSAudio();
        };
        EventsOn("tts:audio", handler);
        return () => { EventsOff("tts:audio"); };
    }, [playNextTTSAudio]);

    // Cleanup AudioContext on unmount
    useEffect(() => {
        return () => { destroyLevelMeter(meterRefs.current); };
    }, []);

    return { ttsEnabled, setTtsEnabled, ttsPlaying, ttsAudioLevel };
}
