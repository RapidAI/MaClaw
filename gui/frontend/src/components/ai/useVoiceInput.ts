/**
 * useVoiceInput -Toggle-mode voice input for the AI assistant panel.
 *
 * Toggle ON: microphone opens, continuously listens. When a speech segment
 * is detected (adaptive energy-based VAD), it's automatically sent to the
 * backend for transcription. The result is dispatched via onTranscribed callback.
 *
 * Toggle OFF: microphone closes, any in-progress segment is finalized.
 *
 * The frontend VAD uses an adaptive noise floor to distinguish speech from
 * background noise. On mic open, the noise floor starts at a sensible default
 * for headset/earbuds (0.006 RMS). Auto-calibration tracks the minimum RMS
 * across the first few chunks to refine this. If the user has run manual
 * calibration (settings panel), those values take priority and EMA adaptation
 * is disabled -the user's calibration is authoritative.
 *
 * Speech threshold calculation:
 *   - With two-phase calibration: noise + 30% of (speech - noise) gap
 *   - Without: noise floor 脳 3.0 multiplier
 * The 30% bias toward noise is intentional: false positives (noise sent to ASR)
 * are cheap (backend Silero VAD filters them), false negatives (missed speech)
 * are expensive (user's words are lost).
 *
 * The backend also runs Silero VAD on the full audio before ASR for a second
 * layer of noise filtering.
 */
import { useState, useRef, useCallback, useEffect } from "react";
import { TranscribeAudioBase64, IsASRReady, LoadConfig, SpeakPlainText } from "../../../wailsjs/go/main/App";
import { EventsEmit, EventsOn } from "../../../wailsjs/runtime";

export type VoiceInputState = "idle" | "listening" | "transcribing";
export type VoiceInputSource = "hold" | "continuous";

export interface UseVoiceInputResult {
    /** Current state: idle (off), listening (mic open), transcribing (processing a segment) */
    state: VoiceInputState;
    /** Whether ASR is available (model downloaded + enabled) */
    asrReady: boolean;
    /** Toggle voice input on/off */
    toggle: () => Promise<void>;
    /** Start push-to-talk recording. */
    startHold: () => Promise<void>;
    /** Stop push-to-talk recording and transcribe immediately. */
    stopHold: () => void;
    /** True while push-to-talk recording is active. */
    holdRecording: boolean;
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

// 鈹€鈹€ Constants 鈹€鈹€
const TARGET_SAMPLE_RATE = 16000;
const CHUNK_SIZE = 4096; // ~256ms at 16kHz

// Energy VAD thresholds for speech segmentation
const SILENCE_THRESHOLD_FLOOR = 0.003; // absolute minimum RMS -below this is always silence (near-zero signal)
const NOISE_CALIBRATION_CHUNKS = 4;    // first N chunks used to calibrate noise floor (~1s at 256ms/chunk)
const NOISE_FLOOR_MULTIPLIER = 3.0;    // speech threshold = noiseFloor * multiplier (SNR requirement)
const NOISE_FLOOR_ADAPT_RATE = 0.05;   // EMA alpha for noise floor adaptation during silence
const NOISE_FLOOR_MAX = 0.08;          // cap noise floor -above this the environment is too loud
const NOISE_FLOOR_DEFAULT = 0.0025;    // conservative default; backend VAD catches false positives better than missed speech
const SPEECH_START_CHUNKS = 2;         // consecutive speech chunks to start collecting a segment
const SILENCE_END_CHUNKS = 6;          // consecutive silence chunks to end a segment (~1.5s)
const SILENCE_FALLBACK_FLUSH_CHUNKS = 12; // prolonged silence drops low-level pre-roll audio (~3s)
const CONTINUOUS_PREROLL_CHUNKS = 6;         // keep ~1.5s of raw audio before VAD confirms speech
const MIN_SPEECH_CHUNKS = 2;           // continuous mode should accept short voice commands
const MIN_CONTINUOUS_SPEECH_SEC = 0.45; // avoid tiny blips, but do not drop short Chinese commands
const MAX_SEGMENT_SEC = 30;            // max segment duration before forced cut
const LOW_CONFIDENCE_AUDIO_MULTIPLIER = 1.15; // below speech threshold, but likely more than room noise
const LOW_CONFIDENCE_AUDIO_FLOOR = 0.0012;
const HOLD_AUDIO_GATE_MULTIPLIER = 0.85;
const HOLD_AUDIO_GATE_FLOOR = 0.0012;
const HOLD_AUDIO_MIN_RMS = 0.0015;
const HOLD_AUDIO_MIN_PEAK = 0.012;
const HOLD_AUDIO_MIN_ACTIVE_CHUNKS = 1;
const ASR_MIN_DURATION_SEC = 0.35;
const ASR_MIN_RMS = 0.0012;
const ASR_MIN_PEAK = 0.01;
const CONTINUOUS_MIN_VOICE_ZERO_CROSSING_RATE = 0.015;
const CONTINUOUS_MAX_VOICE_ZERO_CROSSING_RATE = 0.28;
const CONTINUOUS_MIN_DYNAMIC_RANGE_RATIO = 1.8;
const ASR_TARGET_RMS = 0.035;
const ASR_MAX_GAIN = 6;
const ASR_TRIM_FRAME_SAMPLES = 320;    // 20ms at 16kHz
const ASR_TRIM_PAD_SAMPLES = 1600;     // keep 100ms around detected speech
const ASR_HIGHPASS_CUTOFF_HZ = 80;
const ASR_NOISE_GATE_FLOOR = 0.001;
const ASR_NOISE_GATE_MULTIPLIER = 0.9;
const ASR_NOISE_GATE_SOFT_OPEN_MULTIPLIER = 2.6;
const ASR_NOISE_GATE_MIN_GAIN = 0.22;
const ASR_DESPIKE_ABS_THRESHOLD = 0.92;
const ASR_DESPIKE_NEIGHBOR_MAX = 0.35;
const ASR_ADVANCED_CLEANUP_ENABLED = false;
const VOICE_INPUT_DEBUG = true;

function petRetryPromptText(): string {
    return "\u6ca1\u542c\u6e05\uff0c\u8bf7\u518d\u8bf4\u4e00\u904d\u3002";
}

function emitPetState(state: "idle" | "listening" | "thinking" | "speaking", source: string, ttlMs?: number) {
    try {
        EventsEmit("pet:state", { state, source, ttlMs });
    } catch {
        // Runtime events are best-effort; voice input should never fail because the pet view is absent.
    }
}

function voiceDebug(event: string, detail?: Record<string, unknown>) {
    if (!VOICE_INPUT_DEBUG) return;
    if (detail) {
        console.info(`[voice-input] ${event}`, detail);
    } else {
        console.info(`[voice-input] ${event}`);
    }
}

// 鈹€鈹€ WAV encoding utilities 鈹€鈹€

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

function inspectEncodedWAV(buf: ArrayBuffer): { ok: boolean; channels: number; sampleRate: number; bitsPerSample: number; dataBytes: number; reason?: string } {
    if (buf.byteLength < 44) return { ok: false, channels: 0, sampleRate: 0, bitsPerSample: 0, dataBytes: 0, reason: "too short" };
    const v = new DataView(buf);
    const tag = (off: number, len: number) => {
        let s = "";
        for (let i = 0; i < len; i++) s += String.fromCharCode(v.getUint8(off + i));
        return s;
    };
    const channels = v.getUint16(22, true);
    const sampleRate = v.getUint32(24, true);
    const bitsPerSample = v.getUint16(34, true);
    const dataBytes = v.getUint32(40, true);
    const ok = tag(0, 4) === "RIFF" && tag(8, 4) === "WAVE" && tag(12, 4) === "fmt " && tag(36, 4) === "data" &&
        channels === 1 && sampleRate === TARGET_SAMPLE_RATE && bitsPerSample === 16 && dataBytes === buf.byteLength - 44;
    return { ok, channels, sampleRate, bitsPerSample, dataBytes, reason: ok ? undefined : "unexpected WAV header" };
}

function mixAudioBufferToMono(buffer: AudioBuffer): Float32Array {
    const channels = buffer.numberOfChannels;
    if (channels <= 1) return new Float32Array(buffer.getChannelData(0));

    const length = buffer.length;
    const mono = new Float32Array(length);
    for (let ch = 0; ch < channels; ch++) {
        const data = buffer.getChannelData(ch);
        for (let i = 0; i < length; i++) mono[i] += data[i] / channels;
    }
    return mono;
}

function downsample(buf: Float32Array, srcRate: number, dstRate: number): Float32Array {
    if (srcRate === dstRate) return buf;
    const ratio = srcRate / dstRate;
    const len = Math.round(buf.length / ratio);
    const out = new Float32Array(len);
    if (ratio < 1) {
        for (let i = 0; i < len; i++) {
            const pos = i * ratio;
            const idx = Math.floor(pos);
            const frac = pos - idx;
            const s0 = buf[idx] || 0;
            const s1 = idx + 1 < buf.length ? buf[idx + 1] : s0;
            out[i] = s0 + frac * (s1 - s0);
        }
        return out;
    }
    for (let i = 0; i < len; i++) {
        const start = i * ratio;
        const end = Math.min(buf.length, (i + 1) * ratio);
        const first = Math.floor(start);
        const last = Math.min(buf.length - 1, Math.ceil(end) - 1);
        let sum = 0;
        let weight = 0;
        for (let idx = first; idx <= last; idx++) {
            const sampleStart = Math.max(start, idx);
            const sampleEnd = Math.min(end, idx + 1);
            const w = Math.max(0, sampleEnd - sampleStart);
            sum += (buf[idx] || 0) * w;
            weight += w;
        }
        out[i] = weight > 0 ? sum / weight : 0;
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

function audioStats(data: Float32Array): { rms: number; peak: number } {
    if (data.length === 0) return { rms: 0, peak: 0 };
    let sum = 0;
    let peak = 0;
    for (let i = 0; i < data.length; i++) {
        const sample = data[i];
        const abs = Math.abs(sample);
        if (abs > peak) peak = abs;
        sum += sample * sample;
    }
    return { rms: Math.sqrt(sum / data.length), peak };
}

function voiceLikeStats(data: Float32Array): { zeroCrossingRate: number; dynamicRangeRatio: number } {
    if (data.length < ASR_TRIM_FRAME_SAMPLES * 2) {
        return { zeroCrossingRate: 0, dynamicRangeRatio: 0 };
    }
    let crossings = 0;
    let prev = data[0];
    for (let i = 1; i < data.length; i++) {
        const cur = data[i];
        if ((prev < 0 && cur >= 0) || (prev >= 0 && cur < 0)) crossings++;
        if (cur !== 0) prev = cur;
    }

    const frameValues: number[] = [];
    for (let start = 0; start < data.length; start += ASR_TRIM_FRAME_SAMPLES) {
        const value = rms(data.subarray(start, Math.min(data.length, start + ASR_TRIM_FRAME_SAMPLES)));
        if (Number.isFinite(value) && value > 0) frameValues.push(value);
    }
    if (frameValues.length === 0) {
        return { zeroCrossingRate: crossings / Math.max(1, data.length - 1), dynamicRangeRatio: 0 };
    }
    frameValues.sort((a, b) => a - b);
    const low = frameValues[Math.floor(frameValues.length * 0.2)] || frameValues[0];
    const high = frameValues[Math.min(frameValues.length - 1, Math.floor(frameValues.length * 0.8))] || frameValues[frameValues.length - 1];
    return {
        zeroCrossingRate: crossings / Math.max(1, data.length - 1),
        dynamicRangeRatio: high / Math.max(low, 0.000001),
    };
}

function shouldRejectContinuousAudioForASR(pcm: Float32Array): { reject: boolean; reason?: string; zeroCrossingRate: number; dynamicRangeRatio: number } {
    const stats = voiceLikeStats(pcm);
    if (stats.zeroCrossingRate < CONTINUOUS_MIN_VOICE_ZERO_CROSSING_RATE) {
        return { reject: true, reason: "low zero-crossing rate", ...stats };
    }
    if (stats.zeroCrossingRate > CONTINUOUS_MAX_VOICE_ZERO_CROSSING_RATE) {
        return { reject: true, reason: "high zero-crossing rate", ...stats };
    }
    if (stats.dynamicRangeRatio < CONTINUOUS_MIN_DYNAMIC_RANGE_RATIO && pcm.length / TARGET_SAMPLE_RATE >= 0.8) {
        return { reject: true, reason: "flat energy contour", ...stats };
    }
    return { reject: false, ...stats };
}

function removeDCOffset(pcm: Float32Array): { pcm: Float32Array; offset: number } {
    if (pcm.length === 0) return { pcm, offset: 0 };
    let sum = 0;
    for (let i = 0; i < pcm.length; i++) sum += pcm[i];
    const offset = sum / pcm.length;
    if (Math.abs(offset) < 0.0001) return { pcm, offset };

    const out = new Float32Array(pcm.length);
    for (let i = 0; i < pcm.length; i++) out[i] = pcm[i] - offset;
    return { pcm: out, offset };
}

function highPassPCM(pcm: Float32Array, sampleRate: number): Float32Array {
    if (pcm.length < 2) return pcm;
    const rc = 1 / (2 * Math.PI * ASR_HIGHPASS_CUTOFF_HZ);
    const dt = 1 / sampleRate;
    const alpha = rc / (rc + dt);
    const out = new Float32Array(pcm.length);
    out[0] = pcm[0];
    for (let i = 1; i < pcm.length; i++) {
        out[i] = alpha * (out[i - 1] + pcm[i] - pcm[i - 1]);
    }
    return out;
}

function despikePCM(pcm: Float32Array): { pcm: Float32Array; spikes: number } {
    if (pcm.length < 3) return { pcm, spikes: 0 };
    let out: Float32Array | null = null;
    let spikes = 0;
    for (let i = 1; i < pcm.length - 1; i++) {
        const prev = pcm[i - 1];
        const sample = pcm[i];
        const next = pcm[i + 1];
        const oppositeNeighbors = Math.sign(sample - prev) !== Math.sign(next - sample);
        const isolatedSpike = Math.abs(sample) >= ASR_DESPIKE_ABS_THRESHOLD &&
            Math.abs(prev) <= ASR_DESPIKE_NEIGHBOR_MAX &&
            Math.abs(next) <= ASR_DESPIKE_NEIGHBOR_MAX &&
            oppositeNeighbors;
        if (isolatedSpike) {
            if (!out) out = new Float32Array(pcm);
            out[i] = (prev + next) / 2;
            spikes++;
        }
    }
    return { pcm: out ?? pcm, spikes };
}

function estimateNoiseRMS(pcm: Float32Array): number {
    if (pcm.length === 0) return 0;
    const frameRMS: number[] = [];
    for (let start = 0; start < pcm.length; start += ASR_TRIM_FRAME_SAMPLES) {
        const end = Math.min(pcm.length, start + ASR_TRIM_FRAME_SAMPLES);
        const value = rms(pcm.subarray(start, end));
        if (value > 0 && Number.isFinite(value)) frameRMS.push(value);
    }
    if (frameRMS.length === 0) return 0;
    frameRMS.sort((a, b) => a - b);
    const quietCount = Math.max(1, Math.ceil(frameRMS.length * 0.2));
    let sum = 0;
    for (let i = 0; i < quietCount; i++) sum += frameRMS[i];
    return sum / quietCount;
}

function softGatePCM(pcm: Float32Array, noiseRMS: number): { pcm: Float32Array; gate: number; activeRatio: number } {
    if (pcm.length === 0 || noiseRMS <= 0) return { pcm, gate: 0, activeRatio: 1 };
    const gate = Math.max(ASR_NOISE_GATE_FLOOR, noiseRMS * ASR_NOISE_GATE_MULTIPLIER);
    const openGate = Math.max(gate, noiseRMS * ASR_NOISE_GATE_SOFT_OPEN_MULTIPLIER);
    const out = new Float32Array(pcm.length);
    let activeFrames = 0;
    let totalFrames = 0;
    let previousGain = 1;

    for (let start = 0; start < pcm.length; start += ASR_TRIM_FRAME_SAMPLES) {
        const end = Math.min(pcm.length, start + ASR_TRIM_FRAME_SAMPLES);
        const frame = pcm.subarray(start, end);
        const frameRMS = rms(frame);
        let gain = 1;
        if (frameRMS < openGate) {
            const t = openGate > gate ? Math.max(0, Math.min(1, (frameRMS - gate) / (openGate - gate))) : 0;
            gain = ASR_NOISE_GATE_MIN_GAIN + (1 - ASR_NOISE_GATE_MIN_GAIN) * t;
        }
        if (gain > ASR_NOISE_GATE_MIN_GAIN + 0.05) activeFrames++;
        totalFrames++;
        const frameLen = Math.max(1, end - start);
        for (let i = start; i < end; i++) {
            const t = (i - start + 1) / frameLen;
            const smoothedGain = previousGain + (gain - previousGain) * t;
            out[i] = pcm[i] * smoothedGain;
        }
        previousGain = gain;
    }

    return { pcm: out, gate, activeRatio: totalFrames > 0 ? activeFrames / totalFrames : 1 };
}

function preprocessPCMForASR(pcm: Float32Array, sampleRate: number): {
    pcm: Float32Array;
    dcOffset: number;
    noiseRMS: number;
    noiseGate: number;
    activeRatio: number;
    spikes: number;
} {
    const dc = removeDCOffset(pcm);
    if (!ASR_ADVANCED_CLEANUP_ENABLED) {
        return {
            pcm: dc.pcm,
            dcOffset: dc.offset,
            noiseRMS: 0,
            noiseGate: 0,
            activeRatio: 1,
            spikes: 0,
        };
    }
    const despiked = despikePCM(dc.pcm);
    const highPassed = highPassPCM(despiked.pcm, sampleRate);
    const noiseRMS = estimateNoiseRMS(highPassed);
    const gated = softGatePCM(highPassed, noiseRMS);
    return {
        pcm: gated.pcm,
        dcOffset: dc.offset,
        noiseRMS,
        noiseGate: gated.gate,
        activeRatio: gated.activeRatio,
        spikes: despiked.spikes,
    };
}

function trimPCMForASR(pcm: Float32Array): Float32Array {
    if (pcm.length <= ASR_TRIM_PAD_SAMPLES * 2) return pcm;

    const whole = audioStats(pcm);
    const gate = Math.max(ASR_MIN_RMS, whole.rms * 0.35);
    let first = -1;
    let last = -1;

    for (let start = 0; start < pcm.length; start += ASR_TRIM_FRAME_SAMPLES) {
        const end = Math.min(pcm.length, start + ASR_TRIM_FRAME_SAMPLES);
        if (rms(pcm.subarray(start, end)) >= gate) {
            first = start;
            break;
        }
    }
    for (let start = Math.max(0, pcm.length - ASR_TRIM_FRAME_SAMPLES); start >= 0; start -= ASR_TRIM_FRAME_SAMPLES) {
        const end = Math.min(pcm.length, start + ASR_TRIM_FRAME_SAMPLES);
        if (rms(pcm.subarray(start, end)) >= gate) {
            last = end;
            break;
        }
        if (start === 0) break;
    }

    if (first < 0 || last <= first) return pcm;
    const paddedStart = Math.max(0, first - ASR_TRIM_PAD_SAMPLES);
    const paddedEnd = Math.min(pcm.length, last + ASR_TRIM_PAD_SAMPLES);
    return pcm.slice(paddedStart, paddedEnd);
}

function normalizePCMForASR(pcm: Float32Array): { pcm: Float32Array; gain: number; rms: number; peak: number } {
    const stats = audioStats(pcm);
    if (stats.rms <= 0 || stats.peak <= 0) return { pcm, gain: 1, ...stats };

    const rmsGain = ASR_TARGET_RMS / stats.rms;
    const peakGain = 0.95 / stats.peak;
    const gain = Math.max(1, Math.min(ASR_MAX_GAIN, rmsGain, peakGain));
    if (gain <= 1.05) return { pcm, gain: 1, ...stats };

    const normalized = new Float32Array(pcm.length);
    for (let i = 0; i < pcm.length; i++) {
        normalized[i] = Math.max(-1, Math.min(1, pcm[i] * gain));
    }
    return { pcm: normalized, gain, ...audioStats(normalized) };
}

function pushContinuousPrerollChunk(chunks: Float32Array[], data: Float32Array) {
    chunks.push(new Float32Array(data));
    while (chunks.length > CONTINUOUS_PREROLL_CHUNKS) chunks.shift();
}

function prepareContinuousPrerollChunks(chunks: Float32Array[]): Float32Array[] {
    if (chunks.length === 0) return [];
    return chunks.slice(Math.max(0, chunks.length - CONTINUOUS_PREROLL_CHUNKS));
}

function prepareHoldChunks(chunks: Float32Array[], noiseFloor: number): { chunks: Float32Array[]; reason?: string; stats: Record<string, number> } {
    if (chunks.length === 0) {
        return { chunks: [], reason: "empty", stats: { rawChunks: 0 } };
    }

    const energies = chunks.map(rms);
    const gate = Math.max(HOLD_AUDIO_GATE_FLOOR, noiseFloor * HOLD_AUDIO_GATE_MULTIPLIER);
    const first = energies.findIndex(e => e > gate);
    let last = -1;
    for (let i = energies.length - 1; i >= 0; i--) {
        if (energies[i] > gate) {
            last = i;
            break;
        }
    }

    let peak = 0;
    let sumSquares = 0;
    let totalSamples = 0;
    for (const chunk of chunks) {
        totalSamples += chunk.length;
        for (let i = 0; i < chunk.length; i++) {
            const abs = Math.abs(chunk[i]);
            if (abs > peak) peak = abs;
            sumSquares += chunk[i] * chunk[i];
        }
    }

    const overallRMS = totalSamples > 0 ? Math.sqrt(sumSquares / totalSamples) : 0;
    const activeChunks = energies.filter(e => e > gate).length;
    const stats = {
        rawChunks: chunks.length,
        gate: Number(gate.toFixed(6)),
        activeChunks,
        rms: Number(overallRMS.toFixed(6)),
        peak: Number(peak.toFixed(6)),
    };

    if (first < 0 || activeChunks < HOLD_AUDIO_MIN_ACTIVE_CHUNKS) {
        return { chunks: [], reason: "no active audio", stats };
    }
    if (overallRMS < HOLD_AUDIO_MIN_RMS || peak < HOLD_AUDIO_MIN_PEAK) {
        return { chunks: [], reason: "low energy", stats };
    }

    const start = Math.max(0, first - 1);
    const end = Math.min(chunks.length - 1, last + 1);
    const trimmed = chunks.slice(start, end + 1);
    return {
        chunks: trimmed,
        stats: {
            ...stats,
            trimmedChunks: trimmed.length,
        },
    };
}

interface PendingSpeechSegment {
    chunks: Float32Array[];
    sampleRate: number;
    sampleCount: number;
    speechCount: number;
}

function isStableContinuousSpeech(segment: Pick<PendingSpeechSegment, "sampleRate" | "sampleCount" | "speechCount">): boolean {
    return segment.speechCount >= MIN_SPEECH_CHUNKS &&
        segment.sampleCount / segment.sampleRate >= MIN_CONTINUOUS_SPEECH_SEC;
}

// 鈹€鈹€ Hook 鈹€鈹€

export function useVoiceInput(
    onTranscribed: (text: string, source: VoiceInputSource) => void | Promise<void>,
    inputDeviceId?: string,
): UseVoiceInputResult {
    const [state, setState] = useState<VoiceInputState>("idle");
    const [asrReady, setAsrReady] = useState(false);
    const [duration, setDuration] = useState(0);
    const [isSpeaking, setIsSpeaking] = useState(false);
    const [segmentCount, setSegmentCount] = useState(0);
    const [error, setError] = useState<string | null>(null);
    const [holdRecording, setHoldRecording] = useState(false);

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
    const segmentSamplesRef = useRef(0); // incremental counter -avoids O(N) reduce per chunk
    const segmentSpeechChunksRef = useRef(0); // count of speech chunks in current segment
    const speechChunkCountRef = useRef(0);
    const silenceChunkCountRef = useRef(0);
    const inSpeechRef = useRef(false);
    const fallbackChunksRef = useRef<Float32Array[]>([]);
    const continuousPrerollChunksRef = useRef<Float32Array[]>([]);
    const fallbackSpeechLikeChunksRef = useRef(0);
    const fallbackSilenceChunksRef = useRef(0);
    const holdModeRef = useRef(false);
    const holdChunksRef = useRef<Float32Array[]>([]);
    const holdCancelRequestedRef = useRef(false);
    const pendingTranscriptionsRef = useRef(0); // count of in-flight transcription promises
    const transcriptionQueueRef = useRef<Promise<void>>(Promise.resolve());
    const activeRef = useRef(false); // true while mic is open
    const startingRef = useRef(false); // true during getUserMedia -prevents double-start
    const audioLevelCallbackRef = useRef<((level: number) => void) | null>(null);
    const inputDeviceIdRef = useRef(inputDeviceId);
    inputDeviceIdRef.current = inputDeviceId;
    const onTranscribedRef = useRef(onTranscribed);
    onTranscribedRef.current = onTranscribed;

    // Adaptive noise floor state
    const noiseFloorRef = useRef(NOISE_FLOOR_DEFAULT); // start with headset default, refined by calibration
    const noiseCalibCountRef = useRef(0);      // chunks processed during auto-calibration phase
    const noiseCalibMinRef = useRef(Infinity);  // minimum RMS seen during auto-calibration (robust to speech-during-calibration)
    const noiseCalibDoneRef = useRef(false);   // true after calibration phase completes
    const persistedNoiseFloorRef = useRef(0);  // user-calibrated value from config (0 = not calibrated)
    const persistedSpeechLevelRef = useRef(0); // user-calibrated speech energy from config (0 = not calibrated)
    const petAutoRetryOnNoHearRef = useRef(false);
    const petVoiceReadbackEnabledRef = useRef(false);
    const lastPetRetryPromptAtRef = useRef(0);
    const configLoadedRef = useRef(false);     // true after LoadConfig completes -prevents race with early mic open

    // Load persisted calibration from config on mount.
    // configLoadedRef gates processChunk's calibration path to prevent the race
    // where auto-calibration completes before LoadConfig returns.
    useEffect(() => {
        const loadVoiceConfig = () => {
            LoadConfig().then(cfg => {
                const nf = (cfg as any).noise_floor_calibrated;
                if (typeof nf === 'number' && nf > 0) {
                    persistedNoiseFloorRef.current = nf;
                }
                const sl = (cfg as any).speech_level_calibrated;
                if (typeof sl === 'number' && sl > 0) {
                    persistedSpeechLevelRef.current = sl;
                }
                petAutoRetryOnNoHearRef.current = !!(cfg as any).pet_auto_retry_on_no_hear;
                petVoiceReadbackEnabledRef.current = !!(cfg as any).pet_voice_readback_enabled && ((cfg as any).pet_readback_mode || 'summary') !== 'off';
                configLoadedRef.current = true;
            }).catch(() => {
                configLoadedRef.current = true; // proceed with defaults on error
            });
        };
        loadVoiceConfig();
        const offConfigChanged = EventsOn('config-changed', loadVoiceConfig);
        const offConfigUpdated = EventsOn('config-updated', loadVoiceConfig);
        return () => {
            if (typeof offConfigChanged === 'function') offConfigChanged();
            if (typeof offConfigUpdated === 'function') offConfigUpdated();
        };
    }, []);

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

    const promptPetRetryOnNoHear = useCallback((reason: string) => {
        if (!petAutoRetryOnNoHearRef.current || !petVoiceReadbackEnabledRef.current) return;
        const now = Date.now();
        if (now - lastPetRetryPromptAtRef.current < 5000) return;
        lastPetRetryPromptAtRef.current = now;
        emitPetState("speaking", `asr:no-hear:${reason}`, 3200);
        SpeakPlainText(petRetryPromptText()).catch(() => {});
    }, []);

    /** Send a completed speech segment for transcription. */
    const transcribeSegment = useCallback((chunks: Float32Array[], sampleRate: number, source: VoiceInputSource) => {
        if (chunks.length === 0) {
            voiceDebug("skip transcribe: empty chunk list");
            return;
        }
        const total = chunks.reduce((s, c) => s + c.length, 0);
        const merged = new Float32Array(total);
        let off = 0;
        for (const c of chunks) { merged.set(c, off); off += c.length; }

        const downsampled = downsample(merged, sampleRate, TARGET_SAMPLE_RATE);
        const beforeStats = audioStats(downsampled);
        const preprocessed = preprocessPCMForASR(downsampled, TARGET_SAMPLE_RATE);
        const preprocessedStats = audioStats(preprocessed.pcm);
        const trimmed = trimPCMForASR(preprocessed.pcm);
        const trimmedStats = audioStats(trimmed);
        const normalized = normalizePCMForASR(trimmed);
        const pcm = normalized.pcm;
        const durationSec = pcm.length / TARGET_SAMPLE_RATE;

        // Too short -skip
        if (durationSec < ASR_MIN_DURATION_SEC) {
            voiceDebug("skip transcribe: audio too short", {
                chunks: chunks.length,
                sourceSampleRate: sampleRate,
                samples: pcm.length,
                durationSec: Number(durationSec.toFixed(3)),
            });
            promptPetRetryOnNoHear("too-short");
            return;
        }

        if (normalized.rms < ASR_MIN_RMS || normalized.peak < ASR_MIN_PEAK) {
            voiceDebug("skip transcribe: audio below quality gate", {
                chunks: chunks.length,
                sourceSampleRate: sampleRate,
                samples: pcm.length,
                durationSec: Number(durationSec.toFixed(3)),
                rms: Number(normalized.rms.toFixed(6)),
                peak: Number(normalized.peak.toFixed(6)),
                requiredRMS: ASR_MIN_RMS,
                requiredPeak: ASR_MIN_PEAK,
            });
            promptPetRetryOnNoHear("quality-gate");
            return;
        }

        const voiceShape = source === "continuous" ? shouldRejectContinuousAudioForASR(pcm) : null;
        if (voiceShape?.reject) {
            voiceDebug("skip continuous ASR: audio does not look voice-like", {
                reason: voiceShape.reason,
                zeroCrossingRate: Number(voiceShape.zeroCrossingRate.toFixed(4)),
                dynamicRangeRatio: Number(voiceShape.dynamicRangeRatio.toFixed(2)),
                durationSec: Number(durationSec.toFixed(3)),
            });
            promptPetRetryOnNoHear("voice-shape");
            return;
        }

        const wav = encodeWAV(pcm, TARGET_SAMPLE_RATE);
        const wavInfo = inspectEncodedWAV(wav);
        if (!wavInfo.ok) {
            voiceDebug("skip transcribe: encoded WAV header mismatch", wavInfo);
            setErrorAuto(`Transcription: ${wavInfo.reason || "invalid WAV header"}`);
            return;
        }
        const b64 = toBase64(wav);
        const queuedBefore = pendingTranscriptionsRef.current;
        voiceDebug("queue audio for ASR", {
            source,
            chunks: chunks.length,
            queuedBefore,
            sourceSampleRate: sampleRate,
            targetSampleRate: TARGET_SAMPLE_RATE,
            sourceSamples: downsampled.length,
            samples: pcm.length,
            durationSec: Number(durationSec.toFixed(3)),
            wavChannels: wavInfo.channels,
            wavSampleRate: wavInfo.sampleRate,
            wavBits: wavInfo.bitsPerSample,
            wavDataBytes: wavInfo.dataBytes,
            rawRMS: Number(beforeStats.rms.toFixed(6)),
            rawPeak: Number(beforeStats.peak.toFixed(6)),
            dcOffset: Number(preprocessed.dcOffset.toFixed(6)),
            spikes: preprocessed.spikes,
            noiseRMS: Number(preprocessed.noiseRMS.toFixed(6)),
            noiseGate: Number(preprocessed.noiseGate.toFixed(6)),
            activeRatio: Number(preprocessed.activeRatio.toFixed(3)),
            preRMS: Number(preprocessedStats.rms.toFixed(6)),
            prePeak: Number(preprocessedStats.peak.toFixed(6)),
            trimmedRMS: Number(trimmedStats.rms.toFixed(6)),
            trimmedPeak: Number(trimmedStats.peak.toFixed(6)),
            gain: Number(normalized.gain.toFixed(2)),
            finalRMS: Number(normalized.rms.toFixed(6)),
            finalPeak: Number(normalized.peak.toFixed(6)),
            zeroCrossingRate: voiceShape ? Number(voiceShape.zeroCrossingRate.toFixed(4)) : undefined,
            dynamicRangeRatio: voiceShape ? Number(voiceShape.dynamicRangeRatio.toFixed(2)) : undefined,
        });

        pendingTranscriptionsRef.current++;
        if (!activeRef.current) {
            setState("transcribing");
        emitPetState("thinking", `asr:${source}`, 15000);
        }

        const runTranscription = async () => {
            voiceDebug("send queued audio to ASR", {
                source,
                queued: pendingTranscriptionsRef.current,
                durationSec: Number(durationSec.toFixed(3)),
            });
            try {
                const text = await TranscribeAudioBase64(b64);
                const trimmed = text.trim();
                voiceDebug("ASR returned", {
                    rawLength: text.length,
                    trimmedLength: trimmed.length,
                    text: trimmed,
                });
                if (trimmed) {
                    voiceDebug("dispatch transcribed text", { text: trimmed, source });
                    await Promise.resolve(onTranscribedRef.current(trimmed, source));
                    setSegmentCount(c => c + 1);
                } else {
                    voiceDebug("ASR returned empty text");
                    promptPetRetryOnNoHear("empty-result");
                }
            } catch (err: any) {
                console.warn("[voice-input] ASR failed", err);
                setErrorAuto(`Transcription: ${err?.message || String(err)}`);
            } finally {
                pendingTranscriptionsRef.current--;
                if (!activeRef.current && pendingTranscriptionsRef.current <= 0) {
                    pendingTranscriptionsRef.current = 0;
                    setState("idle");
                    emitPetState("idle", "asr:done");
                }
            }
        };

        transcriptionQueueRef.current = transcriptionQueueRef.current.then(runTranscription, runTranscription);
    }, [promptPetRetryOnNoHear, setErrorAuto]);

    const resetFallbackBuffer = useCallback(() => {
        fallbackChunksRef.current = [];
        fallbackSpeechLikeChunksRef.current = 0;
        fallbackSilenceChunksRef.current = 0;
    }, []);

    const resetContinuousPreroll = useCallback(() => {
        continuousPrerollChunksRef.current = [];
    }, []);

    const resetHoldBuffer = useCallback(() => {
        holdChunksRef.current = [];
    }, []);

    const flushFallbackBuffer = useCallback(() => {
        if (fallbackChunksRef.current.length === 0) {
            voiceDebug("skip fallback flush: empty buffer");
            return;
        }

        const rawChunks = fallbackChunksRef.current;
        const speechLikeCount = fallbackSpeechLikeChunksRef.current;
        resetFallbackBuffer();

        voiceDebug("drop fallback buffer before stable speech", {
            rawChunks: rawChunks.length,
            speechLikeCount,
            noiseFloor: Number(noiseFloorRef.current.toFixed(6)),
        });
    }, [resetFallbackBuffer]);

    const flushHoldBuffer = useCallback((sampleRate: number) => {
        if (holdChunksRef.current.length === 0) {
            voiceDebug("skip hold flush: empty buffer");
            return;
        }

        const rawChunks = holdChunksRef.current;
        const prepared = prepareHoldChunks(rawChunks, noiseFloorRef.current);
        resetHoldBuffer();

        if (prepared.chunks.length === 0) {
            voiceDebug("skip hold flush: audio quality gate rejected segment", {
                reason: prepared.reason,
                ...prepared.stats,
            });
            return;
        }

        const chunks = prepared.chunks;
        voiceDebug("flush hold buffer", {
            rawChunks: rawChunks.length,
            usedChunks: chunks.length,
            noiseFloor: Number(noiseFloorRef.current.toFixed(6)),
            ...prepared.stats,
        });
        transcribeSegment(chunks, sampleRate, "hold");
    }, [resetHoldBuffer, transcribeSegment]);

    /** Process one audio chunk: adaptive energy VAD for segmentation. */
    const processChunk = useCallback((data: Float32Array) => {
        const energy = rms(data);

        // 鈹€鈹€ Adaptive noise floor calibration & tracking 鈹€鈹€
        //
        // Three-tier priority:
        //   1. User-calibrated value (from settings) -highest trust, no EMA adaptation
        //   2. Auto-calibrated value (from first N chunks) -uses minimum RMS, not average
        //   3. NOISE_FLOOR_DEFAULT (low headset/earbuds baseline) -works immediately
        //
        // Auto-calibration uses the MINIMUM RMS across the calibration window, not the
        // average. This is robust to the user speaking during calibration: the quietest
        // chunk in any N-chunk window represents the noise floor, even if other chunks
        // contain speech. Average would be polluted by speech energy.
        //
        // Config loading race: we don't finalize auto-calibration until configLoadedRef
        // is true, so persisted values always take priority even if LoadConfig is slow.
        if (!noiseCalibDoneRef.current) {
            if (configLoadedRef.current && persistedNoiseFloorRef.current > 0) {
                // Tier 1: user-calibrated -use directly, no auto-calibration needed
                noiseFloorRef.current = persistedNoiseFloorRef.current;
                noiseCalibDoneRef.current = true;
            } else {
                // Tier 2/3: track minimum RMS for auto-calibration while using default
                noiseCalibCountRef.current++;
                if (energy < noiseCalibMinRef.current) {
                    noiseCalibMinRef.current = energy;
                }
                // Only finalize auto-calibration after config has loaded (prevents race)
                if (noiseCalibCountRef.current >= NOISE_CALIBRATION_CHUNKS && configLoadedRef.current) {
                    const measured = noiseCalibMinRef.current;
                    // Use measured minimum if it's in a reasonable range; otherwise keep default
                    if (measured > 0 && measured < NOISE_FLOOR_MAX && isFinite(measured)) {
                        noiseFloorRef.current = measured;
                    }
                    noiseCalibDoneRef.current = true;
                }
                // Fall through to speech detection using current noiseFloorRef (default or just-computed)
            }
        }

        // Dynamic speech threshold:
        //   - With speech calibration: noise + 30% of the gap to speech level
        //     (biased toward noise side -better to send noise to backend Silero VAD
        //      than to miss user speech)
        //   - Without: noise floor 脳 multiplier (fallback)
        //   - Always at least SILENCE_THRESHOLD_FLOOR
        let speechThreshold: number;
        if (persistedSpeechLevelRef.current > 0 && persistedNoiseFloorRef.current > 0) {
            const gap = persistedSpeechLevelRef.current - persistedNoiseFloorRef.current;
            speechThreshold = Math.max(
                SILENCE_THRESHOLD_FLOOR,
                persistedNoiseFloorRef.current + gap * 0.3,
            );
        } else {
            speechThreshold = Math.max(
                SILENCE_THRESHOLD_FLOOR,
                noiseFloorRef.current * NOISE_FLOOR_MULTIPLIER,
            );
        }
        const isSpeech = energy > speechThreshold;
        const lowConfidenceThreshold = Math.max(
            LOW_CONFIDENCE_AUDIO_FLOOR,
            noiseFloorRef.current * LOW_CONFIDENCE_AUDIO_MULTIPLIER,
        );
        const isLowConfidenceAudio = !isSpeech && energy > lowConfidenceThreshold;

        // Adapt noise floor during silence using EMA (exponential moving average).
        // Only adapt when:
        //   - NOT in a speech segment (prevents speech energy from raising the floor)
        //   - No user-calibrated value exists (user calibration is authoritative)
        // Cap to NOISE_FLOOR_MAX.
        if (!isSpeech && !inSpeechRef.current && persistedNoiseFloorRef.current <= 0) {
            noiseFloorRef.current = Math.min(
                NOISE_FLOOR_MAX,
                noiseFloorRef.current * (1 - NOISE_FLOOR_ADAPT_RATE) + energy * NOISE_FLOOR_ADAPT_RATE,
            );
        }

        // Emit audio level for visualization.
        // Show level relative to the speech threshold so the user sees meaningful
        // feedback: bars near zero = background noise, bars jumping up = speech.
        const relativeLevel = speechThreshold > 0
            ? Math.min(1, Math.max(0, (energy - noiseFloorRef.current) / (speechThreshold * 3)))
            : Math.min(1, energy * 10);
        audioLevelCallbackRef.current?.(relativeLevel);

        if (holdModeRef.current) {
            holdChunksRef.current.push(new Float32Array(data));
            setIsSpeaking(isSpeech || isLowConfidenceAudio);
            return;
        }

        pushContinuousPrerollChunk(continuousPrerollChunksRef.current, data);

        if (isSpeech) {
            speechChunkCountRef.current++;
            silenceChunkCountRef.current = 0;
        } else {
            silenceChunkCountRef.current++;
            if (!inSpeechRef.current) {
                speechChunkCountRef.current = 0;
            }
        }

        const rate = audioCtxRef.current?.sampleRate || TARGET_SAMPLE_RATE;

        let startedSpeechThisChunk = false;

        if (!inSpeechRef.current) {
            if (isSpeech || isLowConfidenceAudio || fallbackChunksRef.current.length > 0) {
                fallbackChunksRef.current.push(new Float32Array(data));
                if (isSpeech || isLowConfidenceAudio) {
                    fallbackSpeechLikeChunksRef.current++;
                    fallbackSilenceChunksRef.current = 0;
                } else {
                    fallbackSilenceChunksRef.current++;
                }
            }

            if (!isSpeech && fallbackChunksRef.current.length > 0 && fallbackSilenceChunksRef.current >= SILENCE_FALLBACK_FLUSH_CHUNKS) {
                flushFallbackBuffer();
                speechChunkCountRef.current = 0;
                silenceChunkCountRef.current = 0;
            }
        }

        // Speech start: enough consecutive speech chunks
        if (!inSpeechRef.current && speechChunkCountRef.current >= SPEECH_START_CHUNKS) {
            inSpeechRef.current = true;
            const preroll = prepareContinuousPrerollChunks(continuousPrerollChunksRef.current);
            segmentChunksRef.current = preroll;
            segmentSamplesRef.current = preroll.reduce((sum, chunk) => sum + chunk.length, 0);
            voiceDebug("continuous speech started", {
                prerollChunks: preroll.length,
                prerollSec: Number((segmentSamplesRef.current / rate).toFixed(3)),
            });
            segmentSpeechChunksRef.current = 0;
            resetFallbackBuffer();
            startedSpeechThisChunk = true;
            setIsSpeaking(true);
        }

        // Accumulate during speech (copy buffer -AudioBuffer reuses the underlying array)
        if (inSpeechRef.current) {
            if (!startedSpeechThisChunk || segmentChunksRef.current.length === 0) {
                segmentChunksRef.current.push(new Float32Array(data));
                segmentSamplesRef.current += data.length;
            }
            if (isSpeech) segmentSpeechChunksRef.current++;
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
                resetContinuousPreroll();
                setIsSpeaking(false);

                const segment = {
                    chunks,
                    sampleRate: rate,
                    sampleCount: chunks.reduce((sum, chunk) => sum + chunk.length, 0),
                    speechCount,
                };
                if (isStableContinuousSpeech(segment)) {
                    voiceDebug("transcribe continuous segment after silence", {
                        chunks: segment.chunks.length,
                        speechCount: segment.speechCount,
                        durationSec: Number((segment.sampleCount / segment.sampleRate).toFixed(3)),
                    });
                    transcribeSegment(segment.chunks, segment.sampleRate, "continuous");
                } else {
                    voiceDebug("skip continuous segment: VAD duration too short", {
                        speechCount,
                        requiredSpeechChunks: MIN_SPEECH_CHUNKS,
                        durationSec: Number((segment.sampleCount / segment.sampleRate).toFixed(3)),
                        requiredDurationSec: MIN_CONTINUOUS_SPEECH_SEC,
                    });
                }
            }
        }
    }, [flushFallbackBuffer, resetContinuousPreroll, resetFallbackBuffer, transcribeSegment]);

    const cleanup = useCallback(() => {
        activeRef.current = false;
        if (timerRef.current) { clearInterval(timerRef.current); timerRef.current = null; }
        if (processorRef.current) { processorRef.current.disconnect(); processorRef.current = null; }
        if (sourceRef.current) { sourceRef.current.disconnect(); sourceRef.current = null; }
        if (audioCtxRef.current) { audioCtxRef.current.close().catch(() => {}); audioCtxRef.current = null; }
        if (streamRef.current) { streamRef.current.getTracks().forEach(t => t.stop()); streamRef.current = null; }
    }, []);

    useEffect(() => () => {
        cleanup();
        if (errorTimerRef.current) clearTimeout(errorTimerRef.current);
    }, [cleanup]);

    const finishHoldRecording = useCallback((sampleRate: number) => {
        voiceDebug("finish hold recording", {
            sampleRate,
            chunks: holdChunksRef.current.length,
        });
        holdCancelRequestedRef.current = false;
        holdModeRef.current = false;
        setHoldRecording(false);
        setIsSpeaking(false);
        activeRef.current = false;
        flushHoldBuffer(sampleRate);
        cleanup();
        if (pendingTranscriptionsRef.current <= 0) {
            setState("idle");
            emitPetState("idle", "asr:hold-stop");
        } else {
            emitPetState("thinking", "asr:hold-transcribing", 15000);
        }
        setDuration(0);
    }, [cleanup, flushHoldBuffer]);

    /** Open microphone and start continuous listening. */
    const startListening = useCallback(async () => {
        if (startingRef.current || activeRef.current) {
            voiceDebug("skip start listening: already starting or active", {
                starting: startingRef.current,
                active: activeRef.current,
            });
            return;
        } // prevent double-start
        startingRef.current = true;
        voiceDebug("start listening", {
            holdMode: holdModeRef.current,
            inputDeviceId: inputDeviceIdRef.current || "default",
        });

        setErrorAuto(null);
        setDuration(0);
        setSegmentCount(0);
        setIsSpeaking(false);
        segmentChunksRef.current = [];
        segmentSamplesRef.current = 0;
        segmentSpeechChunksRef.current = 0;
        speechChunkCountRef.current = 0;
        silenceChunkCountRef.current = 0;
        resetFallbackBuffer();
        resetContinuousPreroll();
        inSpeechRef.current = false;
        // Reset adaptive noise floor -start with default, auto-calibration will refine
        noiseFloorRef.current = NOISE_FLOOR_DEFAULT;
        noiseCalibCountRef.current = 0;
        noiseCalibMinRef.current = Infinity;
        noiseCalibDoneRef.current = false;

        try {
            const selectedDevice = inputDeviceIdRef.current;
            const stream = await navigator.mediaDevices.getUserMedia({
                audio: {
                    channelCount: 1,
                    sampleRate: { ideal: TARGET_SAMPLE_RATE },
                    echoCancellation: false,
                    noiseSuppression: false,
                    autoGainControl: false,
                    ...(selectedDevice ? { deviceId: { ideal: selectedDevice } } : {}),
                },
            });
            streamRef.current = stream;
            const trackSettings = stream.getAudioTracks()[0]?.getSettings?.();

            const ctx = new AudioContext({ sampleRate: TARGET_SAMPLE_RATE });
            audioCtxRef.current = ctx;
            const source = ctx.createMediaStreamSource(stream);
            sourceRef.current = source;

            const processor = ctx.createScriptProcessor(CHUNK_SIZE, 1, 1);
            processorRef.current = processor;
            processor.onaudioprocess = (e) => {
                if (!activeRef.current) return;
                processChunk(mixAudioBufferToMono(e.inputBuffer));
            };
            source.connect(processor);
            processor.connect(ctx.destination);

            activeRef.current = true;
            startTimeRef.current = Date.now();
            timerRef.current = setInterval(() => {
                setDuration(Math.floor((Date.now() - startTimeRef.current) / 1000));
            }, 500);

            setState("listening");
            emitPetState("listening", holdModeRef.current ? "asr:hold" : "asr:continuous");
            voiceDebug("listening started", {
                sampleRate: ctx.sampleRate,
                trackSampleRate: trackSettings?.sampleRate,
                trackChannelCount: trackSettings?.channelCount,
                trackEchoCancellation: trackSettings?.echoCancellation,
                trackNoiseSuppression: trackSettings?.noiseSuppression,
                trackAutoGainControl: trackSettings?.autoGainControl,
            });
        } catch (err: any) {
            console.warn("[voice-input] failed to start microphone", err);
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
    }, [cleanup, processChunk, resetContinuousPreroll, resetFallbackBuffer, setErrorAuto]);

    const startHold = useCallback(async () => {
        if (state !== "idle" || startingRef.current || activeRef.current) {
            voiceDebug("skip start hold", {
                state,
                starting: startingRef.current,
                active: activeRef.current,
            });
            return;
        }

        voiceDebug("start hold recording");
        holdModeRef.current = true;
        holdCancelRequestedRef.current = false;
        resetHoldBuffer();
        setHoldRecording(true);

        await startListening();

        if (holdCancelRequestedRef.current) {
            const rate = audioCtxRef.current?.sampleRate || TARGET_SAMPLE_RATE;
            finishHoldRecording(rate);
        } else if (!activeRef.current) {
            voiceDebug("hold start ended without active microphone");
            holdModeRef.current = false;
            setHoldRecording(false);
        }
    }, [finishHoldRecording, resetHoldBuffer, startListening, state]);

    const stopHold = useCallback(() => {
        if (!holdModeRef.current) {
            voiceDebug("skip stop hold: hold mode is inactive");
            return;
        }
        if (startingRef.current && !activeRef.current) {
            voiceDebug("stop hold requested while microphone is still starting");
            holdCancelRequestedRef.current = true;
            setHoldRecording(false);
            return;
        }

        const rate = audioCtxRef.current?.sampleRate || TARGET_SAMPLE_RATE;
        voiceDebug("stop hold recording", {
            sampleRate: rate,
            chunks: holdChunksRef.current.length,
        });
        finishHoldRecording(rate);
    }, [finishHoldRecording]);

    /** Close microphone. Finalize any in-progress segment. */
    const stopListening = useCallback(() => {
        if (holdModeRef.current) {
            stopHold();
            return;
        }

        // Capture sample rate before cleanup closes AudioContext
        const rate = audioCtxRef.current?.sampleRate || TARGET_SAMPLE_RATE;
        voiceDebug("stop listening", {
            sampleRate: rate,
            inSpeech: inSpeechRef.current,
            segmentChunks: segmentChunksRef.current.length,
            fallbackChunks: fallbackChunksRef.current.length,
            prerollChunks: continuousPrerollChunksRef.current.length,
        });

        // Finalize in-progress segment if any
        if (inSpeechRef.current && segmentChunksRef.current.length > 0) {
            const chunks = segmentChunksRef.current;
            const speechCount = segmentSpeechChunksRef.current;
            segmentChunksRef.current = [];
            segmentSamplesRef.current = 0;
            segmentSpeechChunksRef.current = 0;
            inSpeechRef.current = false;
            setIsSpeaking(false);
            const segment = {
                chunks,
                sampleRate: rate,
                sampleCount: chunks.reduce((sum, chunk) => sum + chunk.length, 0),
                speechCount,
            };
            if (isStableContinuousSpeech(segment)) {
                transcribeSegment(chunks, rate, "continuous");
            } else {
                voiceDebug("skip unfinished continuous segment: VAD duration too short", {
                    speechCount,
                    requiredSpeechChunks: MIN_SPEECH_CHUNKS,
                    durationSec: Number((segment.sampleCount / segment.sampleRate).toFixed(3)),
                    requiredDurationSec: MIN_CONTINUOUS_SPEECH_SEC,
                    chunks: chunks.length,
                });
            }
        } else {
            flushFallbackBuffer();
        }
        cleanup();
        if (pendingTranscriptionsRef.current > 0) {
            setState("transcribing");
            emitPetState("thinking", "asr:continuous-transcribing", 15000);
        } else {
            setState("idle");
            emitPetState("idle", "asr:continuous-stop");
        }
        setDuration(0);
    }, [cleanup, flushFallbackBuffer, stopHold, transcribeSegment]);

    const toggle = useCallback(async () => {
        if (state === "listening") {
            stopListening();
        } else if (state === "idle") {
            holdModeRef.current = false;
            setHoldRecording(false);
            resetHoldBuffer();
            await startListening();
        }
        // If transcribing (after stop), ignore -will go idle when done
    }, [resetHoldBuffer, state, startListening, stopListening]);

    return {
        state,
        asrReady,
        toggle,
        startHold,
        stopHold,
        holdRecording,
        duration,
        isSpeaking,
        segmentCount,
        error,
        onAudioLevelRef: audioLevelCallbackRef,
    };
}
