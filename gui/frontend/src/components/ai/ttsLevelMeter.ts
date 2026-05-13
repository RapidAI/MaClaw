/**
 * Web Audio API level metering for TTS playback.
 *
 * Decodes WAV base64 into AudioBuffer and plays through:
 *   AudioBufferSourceNode → AnalyserNode → AudioContext.destination
 *
 * This avoids createMediaElementSource issues with data: URIs in WebView2.
 * Audio device selection is handled via AudioContext.setSinkId (Chromium 110+).
 */

export interface LevelMeterRefs {
    audioCtx: AudioContext | null;
    analyser: AnalyserNode | null;
    sourceNode: AudioBufferSourceNode | null;
    raf: number;
    throttleTs: number;
}

export function createLevelMeterRefs(): LevelMeterRefs {
    return { audioCtx: null, analyser: null, sourceNode: null, raf: 0, throttleTs: 0 };
}

export function stopLevelMeter(refs: LevelMeterRefs): void {
    if (refs.raf) {
        cancelAnimationFrame(refs.raf);
        refs.raf = 0;
    }
    if (refs.sourceNode) {
        try { refs.sourceNode.stop(); } catch { /* ignore */ }
        try { refs.sourceNode.disconnect(); } catch { /* ignore */ }
        refs.sourceNode = null;
    }
}

export function destroyLevelMeter(refs: LevelMeterRefs): void {
    stopLevelMeter(refs);
    if (refs.audioCtx) {
        void refs.audioCtx.close().catch(() => {});
        refs.audioCtx = null;
        refs.analyser = null;
    }
}

/**
 * Decode base64 WAV, play through AudioContext with AnalyserNode for level metering.
 * Returns a cleanup function to stop playback.
 */
export function startLevelMeter(
    refs: LevelMeterRefs,
    b64wav: string,
    audioOutputDeviceId: string | undefined,
    onLevel: (level: number) => void,
    onEnded: () => void,
): void {
    try {
        if (!refs.audioCtx) {
            refs.audioCtx = new AudioContext();
        }
        const ctx = refs.audioCtx;
        if (ctx.state === "suspended") {
            void ctx.resume();
        }

        // Apply output device to AudioContext if supported (Chromium 110+)
        if (audioOutputDeviceId && typeof (ctx as any).setSinkId === "function") {
            void (ctx as any).setSinkId(audioOutputDeviceId).catch(() => {});
        }

        if (!refs.analyser) {
            const analyser = ctx.createAnalyser();
            analyser.fftSize = 256;
            analyser.smoothingTimeConstant = 0.4;
            analyser.connect(ctx.destination);
            refs.analyser = analyser;
        }

        // Stop previous source if any
        stopLevelMeter(refs);

        // Decode base64 WAV to ArrayBuffer
        const binary = atob(b64wav);
        const bytes = new Uint8Array(binary.length);
        for (let i = 0; i < binary.length; i++) {
            bytes[i] = binary.charCodeAt(i);
        }

        void ctx.decodeAudioData(bytes.buffer).then((audioBuffer) => {
            // Create source from decoded buffer
            const source = ctx.createBufferSource();
            source.buffer = audioBuffer;
            source.connect(refs.analyser!);
            refs.sourceNode = source;

            source.onended = () => {
                if (refs.raf) {
                    cancelAnimationFrame(refs.raf);
                    refs.raf = 0;
                }
                onLevel(0);
                refs.sourceNode = null;
                onEnded();
            };

            source.start();

            // Start level metering loop
            const dataArray = new Uint8Array(refs.analyser!.frequencyBinCount);
            const tick = () => {
                if (!refs.sourceNode) {
                    onLevel(0);
                    return;
                }
                // Throttle state updates to ~15fps to reduce re-renders
                const now = performance.now();
                if (now - refs.throttleTs < 66) {
                    refs.raf = requestAnimationFrame(tick);
                    return;
                }
                refs.throttleTs = now;

                const analyser = refs.analyser;
                if (!analyser) { onLevel(0); return; }
                analyser.getByteFrequencyData(dataArray);
                let sum = 0;
                const len = dataArray.length;
                for (let i = 0; i < len; i++) {
                    sum += dataArray[i];
                }
                const avg = sum / len / 255;
                onLevel(avg);
                refs.raf = requestAnimationFrame(tick);
            };
            refs.raf = requestAnimationFrame(tick);
        }).catch(() => {
            // Decode failed — call onEnded so queue advances
            onEnded();
        });
    } catch {
        // Web Audio API not available — degrade gracefully
        onEnded();
    }
}
