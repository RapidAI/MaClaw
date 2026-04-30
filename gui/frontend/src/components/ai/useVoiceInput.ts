/**
 * useVoiceInput — Toggle-mode voice input for the AI assistant panel.
 *
 * Toggle ON: microphone opens, continuously listens. When a speech segment
 * is detected (energy-based VAD), it's automatically sent to the backend
 * for transcription. The result is dispatched via onTranscribed callback.
 *
 * Toggle OFF: microphone closes, any in-progress segment is finalized.
 *
 * The backend also runs Silero VAD on the full audio before ASR, so the
 * frontend VAD here is a lightweight energy-based pre-filter for segmentation
 * (detecting when the user starts/stops talking), not for noise filtering.
 */
import { useState, useRef, useCallback, useEffect } from "react";
import { TranscribeAudioBase64, IsASRReady } from "../../../wailsjs/go/main/App";

export type VoiceInputState = "idle" | "listening" | "transcribing";

export interface UseVoiceInputResult {
    /** Current state: idle (off), listening (mic open), transcribing (processing a segment) */
    state: VoiceInputState;
    /** Whether ASR is available (model downloaded + enabled) */
    asrReady: boolean;
    /** Toggle voice input on/off */
    toggle: () => Promise<void>;
    /** How long the mic has been open (seconds) */
    duration: number;
    /** True when speech is currently detected (visual feedback) */
    isSpeaking: boolean;
    /** Number of segments transcribed in this session */
    segmentCount: number;
    /** Error message (auto-dismisses after 4s) */
    error: string | null;
    /** Ref to a callback that receives audio level (0-1) every ~256ms.
     *  Assign a function to this ref to receive real-time levels for visualization.
     *  Uses ref instead of state to avoid re-renders on every audio chunk. */
    onAudioLevelRef: React.MutableRefObject<((level: number) => void) | null>;
}

// ── Constants ──
const TARGET_SAMPLE_RATE = 16000;
const CHUNK_SIZE = 4096; // ~256ms at 16kHz

// Energy VAD thresholds for speech segmentation
const SILENCE_THRESHOLD = 0.01;   // RMS below this = silence
const SPEECH_START_CHUNKS = 2;    // consecutive speech chunks to start a segment
const SILENCE_END_CHUNKS = 6;     // consecutive silence chunks to end a segment (~1.5s)
const MIN_SPEECH_CHUNKS = 3;      // minimum speech chunks for a valid segment (~0.75s)
const MAX_SEGMENT_SEC = 30;       // max segment duration before forced cut

// ── WAV encoding utilities ──

function encodeWAV(samples: Float32Array, sampleRate: number): ArrayBuffer {
    const bitsPerSample = 16;
    const dataSize = samples.length * 2;
    const buffer = new ArrayBuffer(44 + dataSize);
    const v = new DataView(buffer);
    const w = (off: number, s: string) => { for (let i = 0; i < s.length; i++) v.setUint8(off + i, s.charCodeAt(i)); };
    w(0, "RIFF"); v.setUint32(4, 36 + dataSize, true); w(8, "WAVE");
    w(12, "fmt "); v.setUint32(16, 16, true); v.setUint16(20, 1, true); v.setUint16(22, 1, true);
    v.setUint32(24, sampleRate, true); v.setUint32(28, sampleRate * 2, true);
    v.setUint16(32, 2, true); v.setUint16(34, bitsPerSample, true);
    w(36, "data"); v.setUint32(40, dataSize, true);
    let off = 44;
    for (let i = 0; i < samples.length; i++) {
        const s = Math.max(-1, Math.min(1, samples[i]));
        v.setInt16(off, s < 0 ? s * 0x8000 : s * 0x7FFF, true);
        off += 2;
    }
    return buffer;
}

function downsample(buf: Float32Array, srcRate: number, dstRate: number): Float32Array {
    if (srcRate === dstRate) return buf;
    const ratio = srcRate / dstRate;
    const len = Math.round(buf.length / ratio);
    const out = new Float32Array(len);
    for (let i = 0; i < len; i++) {
        const pos = i * ratio;
        const idx = Math.floor(pos);
        const frac = pos - idx;
        out[i] = (buf[idx] || 0) + frac * ((buf[idx + 1] || 0) - (buf[idx] || 0));
    }
    return out;
}

function toBase64(buf: ArrayBuffer): string {
    const bytes = new Uint8Array(buf);
    let bin = "";
    for (let i = 0; i < bytes.length; i += 8192) {
        bin += String.fromCharCode(...bytes.subarray(i, i + 8192));
    }
    return btoa(bin);
}

/** Compute RMS energy of a float32 audio chunk. */
function rms(data: Float32Array): number {
    let sum = 0;
    for (let i = 0; i < data.length; i++) sum += data[i] * data[i];
    return Math.sqrt(sum / data.length);
}

// ── Hook ──

export function useVoiceInput(
    onTranscribed: (text: string) => void,
    inputDeviceId?: string,
): UseVoiceInputResult {
    const [state, setState] = useState<VoiceInputState>("idle");
    const [asrReady, setAsrReady] = useState(false);
    const [duration, setDuration] = useState(0);
    const [isSpeaking, setIsSpeaking] = useState(false);
    const [segmentCount, setSegmentCount] = useState(0);
    const [error, setError] = useState<string | null>(null);

    const errorTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const setErrorAuto = useCallback((msg: string | null) => {
        if (errorTimerRef.current) { clearTimeout(errorTimerRef.current); errorTimerRef.current = null; }
        setError(msg);
        if (msg) errorTimerRef.current = setTimeout(() => { setError(null); errorTimerRef.current = null; }, 4000);
    }, []);

    // Audio resources
    const audioCtxRef = useRef<AudioContext | null>(null);
    const streamRef = useRef<MediaStream | null>(null);
    const processorRef = useRef<ScriptProcessorNode | null>(null);
    const sourceRef = useRef<MediaStreamAudioSourceNode | null>(null);
    const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);
    const startTimeRef = useRef(0);

    // Segment accumulation state (refs to avoid re-renders on every chunk)
    const segmentChunksRef = useRef<Float32Array[]>([]);
    const segmentSamplesRef = useRef(0); // incremental counter — avoids O(N) reduce per chunk
    const segmentSpeechChunksRef = useRef(0); // count of speech chunks in current segment
    const speechChunkCountRef = useRef(0);
    const silenceChunkCountRef = useRef(0);
    const inSpeechRef = useRef(false);
    const pendingTranscriptionsRef = useRef(0); // count of in-flight transcription promises
    const activeRef = useRef(false); // true while mic is open
    const startingRef = useRef(false); // true during getUserMedia — prevents double-start
    const audioLevelCallbackRef = useRef<((level: number) => void) | null>(null);
    const inputDeviceIdRef = useRef(inputDeviceId);
    inputDeviceIdRef.current = inputDeviceId;
    const onTranscribedRef = useRef(onTranscribed);
    onTranscribedRef.current = onTranscribed;

    // Check ASR readiness
    useEffect(() => {
        let cancelled = false;
        const check = async () => {
            try { if (!cancelled) setAsrReady(await IsASRReady()); }
            catch { if (!cancelled) setAsrReady(false); }
        };
        check();
        const iv = setInterval(check, 30000);
        return () => { cancelled = true; clearInterval(iv); };
    }, []);

    /** Send a completed speech segment for transcription. */
    const transcribeSegment = useCallback((chunks: Float32Array[], sampleRate: number) => {
        if (chunks.length === 0) return;
        const total = chunks.reduce((s, c) => s + c.length, 0);
        const merged = new Float32Array(total);
        let off = 0;
        for (const c of chunks) { merged.set(c, off); off += c.length; }

        const pcm = downsample(merged, sampleRate, TARGET_SAMPLE_RATE);

        // Too short — skip
        if (pcm.length < TARGET_SAMPLE_RATE * 0.5) return;

        const wav = encodeWAV(pcm, TARGET_SAMPLE_RATE);
        const b64 = toBase64(wav);

        pendingTranscriptionsRef.current++;
        if (!activeRef.current) {
            setState("transcribing");
        }

        TranscribeAudioBase64(b64)
            .then((text) => {
                const trimmed = text.trim();
                if (trimmed) {
                    onTranscribedRef.current(trimmed);
                    setSegmentCount(c => c + 1);
                }
            })
            .catch((err) => {
                setErrorAuto(`Transcription: ${err?.message || String(err)}`);
            })
            .finally(() => {
                pendingTranscriptionsRef.current--;
                if (!activeRef.current && pendingTranscriptionsRef.current <= 0) {
                    pendingTranscriptionsRef.current = 0;
                    setState("idle");
                }
            });
    }, [setErrorAuto]);

    /** Process one audio chunk: energy VAD for segmentation. */
    const processChunk = useCallback((data: Float32Array) => {
        const energy = rms(data);
        const isSpeech = energy > SILENCE_THRESHOLD;

        // Emit audio level for visualization (normalized to 0-1, clamped)
        // Scale: typical speech RMS is 0.02-0.15, scale up for visual impact
        const normalizedLevel = Math.min(1, energy * 10);
        audioLevelCallbackRef.current?.(normalizedLevel);

        if (isSpeech) {
            speechChunkCountRef.current++;
            silenceChunkCountRef.current = 0;
        } else {
            silenceChunkCountRef.current++;
            if (!inSpeechRef.current) {
                speechChunkCountRef.current = 0;
            }
        }

        // Speech start: enough consecutive speech chunks
        if (!inSpeechRef.current && speechChunkCountRef.current >= SPEECH_START_CHUNKS) {
            inSpeechRef.current = true;
            segmentChunksRef.current = [];
            segmentSamplesRef.current = 0;
            segmentSpeechChunksRef.current = 0;
            setIsSpeaking(true);
        }

        // Accumulate during speech (copy buffer — AudioBuffer reuses the underlying array)
        if (inSpeechRef.current) {
            segmentChunksRef.current.push(new Float32Array(data));
            segmentSamplesRef.current += data.length;
            if (isSpeech) segmentSpeechChunksRef.current++;

            const rate = audioCtxRef.current?.sampleRate || TARGET_SAMPLE_RATE;
            const segSec = segmentSamplesRef.current / rate;

            // Speech end: enough silence after speech, or max duration
            if (silenceChunkCountRef.current >= SILENCE_END_CHUNKS || segSec >= MAX_SEGMENT_SEC) {
                const chunks = segmentChunksRef.current;
                const speechCount = segmentSpeechChunksRef.current;
                segmentChunksRef.current = [];
                segmentSamplesRef.current = 0;
                segmentSpeechChunksRef.current = 0;
                inSpeechRef.current = false;
                speechChunkCountRef.current = 0;
                silenceChunkCountRef.current = 0;
                setIsSpeaking(false);

                if (speechCount >= MIN_SPEECH_CHUNKS) {
                    transcribeSegment(chunks, rate);
                }
            }
        }
    }, [transcribeSegment]);

    const cleanup = useCallback(() => {
        activeRef.current = false;
        if (timerRef.current) { clearInterval(timerRef.current); timerRef.current = null; }
        if (processorRef.current) { processorRef.current.disconnect(); processorRef.current = null; }
        if (sourceRef.current) { sourceRef.current.disconnect(); sourceRef.current = null; }
        if (audioCtxRef.current) { audioCtxRef.current.close().catch(() => {}); audioCtxRef.current = null; }
        if (streamRef.current) { streamRef.current.getTracks().forEach(t => t.stop()); streamRef.current = null; }
    }, []);

    useEffect(() => () => { cleanup(); if (errorTimerRef.current) clearTimeout(errorTimerRef.current); }, [cleanup]);

    /** Open microphone and start continuous listening. */
    const startListening = useCallback(async () => {
        if (startingRef.current || activeRef.current) return; // prevent double-start
        startingRef.current = true;

        setErrorAuto(null);
        setDuration(0);
        setSegmentCount(0);
        segmentChunksRef.current = [];
        segmentSamplesRef.current = 0;
        segmentSpeechChunksRef.current = 0;
        speechChunkCountRef.current = 0;
        silenceChunkCountRef.current = 0;
        inSpeechRef.current = false;

        try {
            const selectedDevice = inputDeviceIdRef.current;
            const stream = await navigator.mediaDevices.getUserMedia({
                audio: {
                    channelCount: 1,
                    sampleRate: { ideal: TARGET_SAMPLE_RATE },
                    echoCancellation: true,
                    noiseSuppression: true,
                    ...(selectedDevice ? { deviceId: { ideal: selectedDevice } } : {}),
                },
            });
            streamRef.current = stream;

            const ctx = new AudioContext({ sampleRate: TARGET_SAMPLE_RATE });
            audioCtxRef.current = ctx;
            const source = ctx.createMediaStreamSource(stream);
            sourceRef.current = source;

            const processor = ctx.createScriptProcessor(CHUNK_SIZE, 1, 1);
            processorRef.current = processor;
            processor.onaudioprocess = (e) => {
                if (!activeRef.current) return;
                processChunk(e.inputBuffer.getChannelData(0));
            };
            source.connect(processor);
            processor.connect(ctx.destination);

            activeRef.current = true;
            startTimeRef.current = Date.now();
            timerRef.current = setInterval(() => {
                setDuration(Math.floor((Date.now() - startTimeRef.current) / 1000));
            }, 500);

            setState("listening");
        } catch (err: any) {
            cleanup();
            const msg = err?.message || String(err);
            if (msg.includes("Permission") || msg.includes("NotAllowed")) {
                setErrorAuto("Microphone access denied");
            } else {
                setErrorAuto(`Mic error: ${msg}`);
            }
        } finally {
            startingRef.current = false;
        }
    }, [cleanup, processChunk, setErrorAuto]);

    /** Close microphone. Finalize any in-progress segment. */
    const stopListening = useCallback(() => {
        // Capture sample rate before cleanup closes AudioContext
        const rate = audioCtxRef.current?.sampleRate || TARGET_SAMPLE_RATE;

        // Finalize in-progress segment if any
        if (inSpeechRef.current && segmentChunksRef.current.length > 0) {
            const chunks = segmentChunksRef.current;
            const speechCount = segmentSpeechChunksRef.current;
            segmentChunksRef.current = [];
            segmentSpeechChunksRef.current = 0;
            inSpeechRef.current = false;
            setIsSpeaking(false);
            if (speechCount >= MIN_SPEECH_CHUNKS) {
                transcribeSegment(chunks, rate);
            }
        }

        cleanup();
        if (pendingTranscriptionsRef.current <= 0) {
            setState("idle");
        }
        setDuration(0);
    }, [cleanup, transcribeSegment]);

    const toggle = useCallback(async () => {
        if (state === "listening") {
            stopListening();
        } else if (state === "idle") {
            await startListening();
        }
        // If transcribing (after stop), ignore — will go idle when done
    }, [state, startListening, stopListening]);

    return {
        state,
        asrReady,
        toggle,
        duration,
        isSpeaking,
        segmentCount,
        error,
        onAudioLevelRef: audioLevelCallbackRef,
    };
}
