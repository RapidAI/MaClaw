import { useCallback, useEffect, useRef, useState } from "react";
import { GetTTSEnabled, SetTTSEnabled } from "../../../wailsjs/go/main/App";
import { EventsOff, EventsOn } from "../../../wailsjs/runtime";

export function useTTSReadback(audioOutputDeviceId?: string) {
    const [ttsEnabled, setTtsEnabledState] = useState(false);
    const ttsEnabledRef = useRef(false);
    const ttsAudioQueueRef = useRef<string[]>([]);
    const ttsAudioPlayingRef = useRef(false);
    const ttsCurrentAudioRef = useRef<HTMLAudioElement | null>(null);

    useEffect(() => { ttsEnabledRef.current = ttsEnabled; }, [ttsEnabled]);
    useEffect(() => { GetTTSEnabled().then(v => setTtsEnabledState(!!v)).catch(() => {}); }, []);

    const setTtsEnabled = useCallback((next: boolean) => {
        setTtsEnabledState(next);
        void SetTTSEnabled(next).catch(() => {});
    }, []);

    const playNextTTSAudio = useCallback(() => {
        if (ttsAudioPlayingRef.current) return;
        if (!ttsEnabledRef.current) {
            ttsAudioQueueRef.current = [];
            return;
        }
        const b64wav = ttsAudioQueueRef.current.shift();
        if (!b64wav) return;
        ttsAudioPlayingRef.current = true;
        try {
            const audio = new Audio("data:audio/wav;base64," + b64wav);
            ttsCurrentAudioRef.current = audio;
            if (audioOutputDeviceId && typeof (audio as any).setSinkId === "function") {
                void (audio as any).setSinkId(audioOutputDeviceId).catch(() => {});
            }
            const finish = () => {
                audio.onended = null;
                audio.onerror = null;
                if (ttsCurrentAudioRef.current === audio) ttsCurrentAudioRef.current = null;
                ttsAudioPlayingRef.current = false;
                playNextTTSAudio();
            };
            audio.onended = finish;
            audio.onerror = finish;
            void audio.play().catch(finish);
        } catch {
            ttsAudioPlayingRef.current = false;
            playNextTTSAudio();
        }
    }, [audioOutputDeviceId]);

    useEffect(() => {
        if (ttsEnabled) return;
        ttsAudioQueueRef.current = [];
        ttsAudioPlayingRef.current = false;
        const audio = ttsCurrentAudioRef.current;
        if (audio) {
            audio.pause();
            audio.src = "";
            ttsCurrentAudioRef.current = null;
        }
    }, [ttsEnabled]);

    useEffect(() => {
        const handler = (b64wav: string) => {
            if (!ttsEnabledRef.current) return;
            ttsAudioQueueRef.current.push(b64wav);
            playNextTTSAudio();
        };
        EventsOn("tts:audio", handler);
        return () => { EventsOff("tts:audio"); };
    }, [playNextTTSAudio]);

    return { ttsEnabled, setTtsEnabled };
}
