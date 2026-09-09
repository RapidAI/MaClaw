/**
 * useAudioDevices — React hook for enumerating audio input/output devices.
 *
 * Uses navigator.mediaDevices.enumerateDevices() to list available microphones
 * and speakers. Refreshes on device change events (USB mic plugged in, etc.).
 *
 * Device labels require microphone permission. Call requestLabels() to trigger
 * a temporary getUserMedia → enumerateDevices cycle that unlocks real labels.
 */
import { useState, useEffect, useCallback, useRef } from "react";

export interface AudioDeviceInfo {
    deviceId: string;
    label: string;
    kind: "audioinput" | "audiooutput";
}

export interface UseAudioDevicesResult {
    /** Available microphones */
    inputs: AudioDeviceInfo[];
    /** Available speakers/headphones */
    outputs: AudioDeviceInfo[];
    /** Re-enumerate devices */
    refresh: () => Promise<void>;
    /** Request mic permission to unlock device labels, then refresh */
    requestLabels: () => Promise<void>;
    /** True if labels have been unlocked (real names, not "Microphone 1") */
    labelsAvailable: boolean;
}

export function useAudioDevices(): UseAudioDevicesResult {
    const [inputs, setInputs] = useState<AudioDeviceInfo[]>([]);
    const [outputs, setOutputs] = useState<AudioDeviceInfo[]>([]);
    const [labelsAvailable, setLabelsAvailable] = useState(false);
    const mountedRef = useRef(true);

    const refresh = useCallback(async () => {
        try {
            const devices = await navigator.mediaDevices.enumerateDevices();
            const ins: AudioDeviceInfo[] = [];
            const outs: AudioDeviceInfo[] = [];
            let inputIdx = 0;
            let outputIdx = 0;
            let hasRealLabel = false;
            for (const d of devices) {
                if (d.kind === "audioinput") {
                    const label = d.label || `Microphone ${++inputIdx}`;
                    if (d.label) hasRealLabel = true;
                    ins.push({ deviceId: d.deviceId, label, kind: "audioinput" });
                } else if (d.kind === "audiooutput") {
                    const label = d.label || `Speaker ${++outputIdx}`;
                    if (d.label) hasRealLabel = true;
                    outs.push({ deviceId: d.deviceId, label, kind: "audiooutput" });
                }
            }
            if (mountedRef.current) {
                setInputs(ins);
                setOutputs(outs);
                setLabelsAvailable(hasRealLabel);
            }
        } catch {
            // enumerateDevices not available
        }
    }, []);

    // Request temporary mic permission to unlock device labels.
    // The stream is immediately stopped — we only need the permission grant.
    const requestLabels = useCallback(async () => {
        try {
            const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
            stream.getTracks().forEach(t => t.stop());
            await refresh();
        } catch {
            // Permission denied — labels stay as "Microphone 1" etc.
        }
    }, [refresh]);

    useEffect(() => {
        mountedRef.current = true;
        refresh();
        const handler = () => { refresh(); };
        navigator.mediaDevices?.addEventListener("devicechange", handler);
        return () => {
            mountedRef.current = false;
            navigator.mediaDevices?.removeEventListener("devicechange", handler);
        };
    }, [refresh]);

    return { inputs, outputs, refresh, requestLabels, labelsAvailable };
}
