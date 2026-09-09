/**
 * Interactive long-form recording UI (waveform + pause/stop).
 * Used when the agent opens a record_audio session.
 *
 * Memory / IPC notes:
 * - Capture prefers AudioWorklet (ScriptProcessor fallback).
 * - Live mode (preferred): BeginLive on mic open → stream PCM to disk during
 *   capture via binary HTTP POST (no base64, no multi-hour JS heap).
 * - Fallback: hold Int16 slabs in memory, bulk-upload on stop (base64 chunks).
 * - Waveform updates throttled to ~10 Hz.
 */
import React, { useCallback, useEffect, useRef, useState } from "react";
import {
    AppendRecordedAudioBase64,
    BeginLiveRecordedAudioUpload,
    BeginRecordedAudioUpload,
    CancelRecordedAudioUpload,
    FinishRecordedAudioUpload,
    LoadConfig,
} from "../../../wailsjs/go/main/App";
import { EventsOn } from "../../../wailsjs/runtime";
import type { Theme } from "./aiAssistantPanelTheme";
import { startRecordCapture, type RecordCaptureHandle } from "./recordAudioCapture";

const TARGET_SAMPLE_RATE = 16000;
const WAVEFORM_BARS = 48;
const MAX_DURATION_SEC = 3 * 60 * 60; // 3 hours hard cap
const WAVEFORM_MIN_INTERVAL_MS = 100;
const MIN_RECORD_SEC = 0.3;
/** Compact many small capture slabs into larger arrays (memory fallback only). */
const COMPACT_CHUNK_THRESHOLD = 48;
/**
 * Classic stop-time upload chunk (~256 KiB binary → ~350 KiB base64).
 * Must stay ≤ backend maxRecordedAudioChunkBytes (512 KiB).
 */
const UPLOAD_CHUNK_BYTES = 256 * 1024;
/**
 * Live capture flush size for binary HTTP append (PCM only).
 * Smaller than UPLOAD_CHUNK_BYTES so disk stays close to the mic timeline.
 */
const LIVE_STREAM_FLUSH_BYTES = 128 * 1024;
/**
 * Max number of in-flight/queued live flush jobs (~128KiB each).
 * Caps JS memory if disk/IPC is slower than realtime capture.
 */
const LIVE_MAX_QUEUED_FLUSHES = 16; // ≤ ~2 MiB of PCM in the upload pipeline
/**
 * Max size of one compacted PCM head during capture (memory fallback).
 * Matches UPLOAD_CHUNK_BYTES so stop/save rarely needs to re-slice heads.
 */
const COMPACT_MAX_HEAD_BYTES = UPLOAD_CHUNK_BYTES;
/** Shared empty slab for progressive GC during upload (avoid per-chunk alloc). */
const EMPTY_I16 = new Int16Array(0);
const DEFAULT_LIVE_APPEND_PATH = "/maclaw-record/v1/append";

/** Non-empty trimmed string from FinishRecordedAudioUpload fields. */
function finishInfoString(v: unknown): string | undefined {
    if (typeof v !== "string") return undefined;
    const s = v.trim();
    return s || undefined;
}

/** Console detail logs only when settings → 日志详情 is enabled. */
function recordDebug(event: string, detail?: Record<string, unknown>, enabled = false) {
    if (!enabled) return;
    if (detail) {
        console.info(`[record-audio] ${event}`, detail);
    } else {
        console.info(`[record-audio] ${event}`);
    }
}

export type RecordingCompleteResult = {
    status: "stopped" | "cancelled" | "error";
    path?: string;
    /** Sibling MP3 archive produced on save (when conversion succeeds). */
    mp3Path?: string;
    mp3SizeBytes?: number;
    /** Present when in-process MP3 archive failed; agent should convert via ffmpeg. */
    mp3Error?: string;
    durationSec?: number;
    sizeBytes?: number;
    format?: string;
    title?: string;
    error?: string;
};

export type RecordingSessionCardProps = {
    title: string;
    purpose?: string;
    theme: Theme;
    lang?: string;
    /** When false, the card is historical and does not capture audio. */
    active?: boolean;
    onComplete: (result: RecordingCompleteResult) => void;
};

/** Write standard mono PCM16 LE WAV header into buffer[0..44). */
function writeWavHeader(buffer: ArrayBuffer, dataSize: number, sampleRate: number) {
    const v = new DataView(buffer);
    const w = (off: number, s: string) => {
        for (let i = 0; i < s.length; i++) v.setUint8(off + i, s.charCodeAt(i));
    };
    w(0, "RIFF");
    v.setUint32(4, 36 + dataSize, true);
    w(8, "WAVE");
    w(12, "fmt ");
    v.setUint32(16, 16, true);
    v.setUint16(20, 1, true);
    v.setUint16(22, 1, true);
    v.setUint32(24, sampleRate, true);
    v.setUint32(28, sampleRate * 2, true);
    v.setUint16(32, 2, true);
    v.setUint16(34, 16, true);
    w(36, "data");
    v.setUint32(40, dataSize, true);
}

/**
 * Encode chunked Int16 PCM directly into a WAV buffer (no intermediate merge array).
 * Peak memory stays closer to chunks+wav instead of chunks+merged+wav.
 * Sample count is derived from chunks when the provided total is inconsistent.
 */
function encodeWAVFromChunks(chunks: Int16Array[], totalSamples: number, sampleRate: number): ArrayBuffer {
    let actual = 0;
    for (const c of chunks) actual += c.length;
    if (actual > 0 && actual !== totalSamples) {
        totalSamples = actual;
    }
    const dataSize = totalSamples * 2;
    const buffer = new ArrayBuffer(44 + dataSize);
    writeWavHeader(buffer, dataSize, sampleRate);
    const dst = new Uint8Array(buffer, 44);
    let off = 0;
    for (const c of chunks) {
        const bytes = new Uint8Array(c.buffer, c.byteOffset, c.byteLength);
        if (off + bytes.length > dst.length) break;
        dst.set(bytes, off);
        off += bytes.length;
    }
    return buffer;
}

/** Encode a single Int16 buffer as WAV (tests / small clips). */
function encodeWAVFromInt16(samples: Int16Array, sampleRate: number): ArrayBuffer {
    return encodeWAVFromChunks([samples], samples.length, sampleRate);
}

function downsampleToInt16(buf: Float32Array, srcRate: number, dstRate: number): Int16Array {
    let mono: Float32Array;
    if (srcRate === dstRate) {
        mono = buf;
    } else {
        const ratio = srcRate / dstRate;
        const len = Math.max(1, Math.round(buf.length / ratio));
        mono = new Float32Array(len);
        for (let i = 0; i < len; i++) {
            const start = Math.floor(i * ratio);
            const end = Math.min(buf.length, Math.floor((i + 1) * ratio));
            let sum = 0;
            let n = 0;
            for (let j = start; j < end; j++) {
                sum += buf[j] || 0;
                n++;
            }
            mono[i] = n > 0 ? sum / n : 0;
        }
    }
    const out = new Int16Array(mono.length);
    for (let i = 0; i < mono.length; i++) {
        const s = Math.max(-1, Math.min(1, mono[i]));
        // Round toward nearest int16; avoid |0 which truncates toward zero.
        let v = s < 0 ? Math.round(s * 0x8000) : Math.round(s * 0x7fff);
        if (v > 32767) v = 32767;
        if (v < -32768) v = -32768;
        out[i] = v;
    }
    return out;
}

/**
 * Merge leading micro-chunks when the list grows large (long recordings).
 * Heads are packed into slabs of at most COMPACT_MAX_HEAD_BYTES so a multi-hour
 * session never collapses into one multi-MB TypedArray (GC + upload risk).
 */
function compactInt16Chunks(
    chunks: Int16Array[],
    maxHeadBytes: number = COMPACT_MAX_HEAD_BYTES,
): Int16Array[] {
    if (chunks.length < COMPACT_CHUNK_THRESHOLD) return chunks;
    const keepTail = 8;
    const headCount = chunks.length - keepTail;
    if (headCount <= 1) return chunks;

    // Even sample count: Int16 → 2 bytes/sample. Floor so maxBytes is never exceeded.
    const maxSamples = Math.max(1, Math.floor(Math.max(2, maxHeadBytes) / 2));
    let remaining = 0;
    for (let i = 0; i < headCount; i++) remaining += chunks[i].length;

    const packed: Int16Array[] = [];
    let buf: Int16Array | null = null;
    let used = 0;
    let capacity = 0;

    const flushBuf = () => {
        if (!buf || used <= 0) {
            buf = null;
            used = 0;
            capacity = 0;
            return;
        }
        packed.push(used === buf.length ? buf : buf.slice(0, used));
        buf = null;
        used = 0;
        capacity = 0;
    };

    for (let i = 0; i < headCount; i++) {
        const src = chunks[i];
        if (!src || src.length === 0) continue;
        let off = 0;
        while (off < src.length) {
            if (!buf) {
                // Size to remaining work, capped by maxSamples — avoid large alloc for tiny heads.
                const leftHere = src.length - off;
                const plan = remaining > 0 ? remaining : leftHere;
                capacity = Math.max(1, Math.min(maxSamples, plan));
                buf = new Int16Array(capacity);
                used = 0;
            }
            const take = Math.min(src.length - off, capacity - used);
            if (take <= 0) {
                // Defensive: avoid infinite loop if capacity tracking ever desyncs.
                flushBuf();
                continue;
            }
            buf.set(src.subarray(off, off + take), used);
            used += take;
            off += take;
            remaining -= take;
            if (used >= capacity) flushBuf();
        }
    }
    flushBuf();
    return packed.length === 0 ? chunks.slice(headCount) : packed.concat(chunks.slice(headCount));
}

/**
 * Chunked base64 without spreading large typed arrays.
 * Accepts ArrayBuffer or a Uint8Array view (encodes only the view range).
 */
function toBase64(buf: ArrayBuffer | Uint8Array): string {
    const bytes = buf instanceof Uint8Array ? buf : new Uint8Array(buf);
    // Keep windows under typical apply() argument limits (~64k).
    const chunk = 0x8000;
    const parts: string[] = [];
    for (let i = 0; i < bytes.length; i += chunk) {
        const end = Math.min(i + chunk, bytes.length);
        // Build a dense number[] for apply (typed-array apply is not portable).
        const codes: number[] = new Array(end - i);
        for (let j = i, k = 0; j < end; j++, k++) codes[k] = bytes[j];
        parts.push(String.fromCharCode.apply(null, codes));
    }
    return btoa(parts.join(""));
}

/** Even binary limit so 16-bit PCM frames never split mid-sample. */
function uploadByteLimit(maxBytes: number): number {
    if (maxBytes <= 0) {
        throw new Error("maxBytes must be positive");
    }
    return maxBytes >= 2 ? maxBytes - (maxBytes % 2) : maxBytes;
}

/**
 * Drive PCM into ≤ maxBytes slabs using one fill buffer.
 * onSlab receives a view into the fill buffer; it must fully consume the bytes
 * (copy or encode) before returning — the buffer is reused after onSlab resolves.
 */
async function forEachPcmUploadSlab(
    chunks: Int16Array[],
    maxBytes: number,
    releaseSource: boolean,
    onSlab: (view: Uint8Array) => void | Promise<void>,
): Promise<number> {
    const limit = uploadByteLimit(maxBytes);
    const work = new Uint8Array(limit);
    let filled = 0;
    let slabs = 0;

    const flush = async () => {
        if (filled <= 0) return;
        const len = filled;
        filled = 0;
        // View valid until onSlab returns (caller must encode/copy synchronously
        // before any await that lets this function resume and overwrite work).
        await onSlab(work.subarray(0, len));
        slabs++;
    };

    for (let i = 0; i < chunks.length; i++) {
        const c = chunks[i];
        if (!c || c.length === 0) {
            if (releaseSource) chunks[i] = EMPTY_I16;
            continue;
        }
        let bytes = new Uint8Array(c.buffer, c.byteOffset, c.byteLength);
        while (bytes.length > 0) {
            const take = Math.min(bytes.length, limit - filled);
            work.set(bytes.subarray(0, take), filled);
            filled += take;
            bytes = bytes.subarray(take);
            if (filled >= limit) await flush();
        }
        if (releaseSource) chunks[i] = EMPTY_I16;
    }
    await flush();
    return slabs;
}

/**
 * Yield PCM upload payloads each ≤ maxBytes (sync generator for tests).
 * Each yield is an independent copy safe to hold across awaits.
 */
function* iteratePcmUploadSlabs(
    chunks: Int16Array[],
    maxBytes: number = UPLOAD_CHUNK_BYTES,
    releaseSource = false,
): Generator<Uint8Array, void, void> {
    const limit = uploadByteLimit(maxBytes);
    const work = new Uint8Array(limit);
    let filled = 0;

    for (let i = 0; i < chunks.length; i++) {
        const c = chunks[i];
        if (!c || c.length === 0) {
            if (releaseSource) chunks[i] = EMPTY_I16;
            continue;
        }
        let bytes = new Uint8Array(c.buffer, c.byteOffset, c.byteLength);
        while (bytes.length > 0) {
            const take = Math.min(bytes.length, limit - filled);
            work.set(bytes.subarray(0, take), filled);
            filled += take;
            bytes = bytes.subarray(take);
            if (filled >= limit) {
                yield work.slice(0, filled);
                filled = 0;
            }
        }
        if (releaseSource) chunks[i] = EMPTY_I16;
    }
    if (filled > 0) yield work.slice(0, filled);
}

/**
 * Stream PCM to backend: encode base64 from the fill-buffer view (sync), then
 * await Append. No extra PCM copy per slab; source chunks released as consumed.
 */
async function appendPcmUploadSlabs(
    sessionId: string,
    chunks: Int16Array[],
    maxBytes: number = UPLOAD_CHUNK_BYTES,
): Promise<number> {
    return forEachPcmUploadSlab(chunks, maxBytes, true, async (view) => {
        // Encode synchronously first so the fill buffer may be reused after await.
        const b64 = toBase64(view);
        await AppendRecordedAudioBase64(sessionId, b64);
    });
}

/** Collect all upload slabs (tests / small clips only). */
function splitPcmForUpload(chunks: Int16Array[], maxBytes: number = UPLOAD_CHUNK_BYTES): Uint8Array[] {
    return Array.from(iteratePcmUploadSlabs(chunks, maxBytes));
}

function isLikelyNetworkFetchError(err: unknown): boolean {
    if (err instanceof TypeError) return true;
    const msg = err instanceof Error ? err.message : String(err);
    return /failed to fetch|networkerror|load failed|econnrefused|network request failed/i.test(msg);
}

/**
 * True binary PCM append via AssetServer (no base64).
 * Falls back to Wails base64 binding only when the binary route is missing
 * (404/405) or the fetch transport itself fails — not on app-level errors
 * (session closed, chunk too large, etc.).
 */
async function appendLivePCM(
    sessionId: string,
    data: Uint8Array,
    appendPath: string = DEFAULT_LIVE_APPEND_PATH,
): Promise<"binary" | "base64"> {
    if (!sessionId || data.length === 0) return "binary";
    const path = appendPath || DEFAULT_LIVE_APPEND_PATH;
    const body =
        data.byteOffset === 0 && data.byteLength === data.buffer.byteLength
            ? data
            : data.slice();

    try {
        const url = `${path}?session_id=${encodeURIComponent(sessionId)}`;
        const res = await fetch(url, {
            method: "POST",
            headers: { "Content-Type": "application/octet-stream" },
            body,
        });
        if (res.ok || res.status === 204) return "binary";
        // Route missing (older build without middleware) → base64 binding.
        if (res.status === 404 || res.status === 405) {
            await AppendRecordedAudioBase64(sessionId, toBase64(data));
            return "base64";
        }
        const text = (await res.text().catch(() => "")).trim();
        throw new Error(text || `append failed: HTTP ${res.status}`);
    } catch (err) {
        if (!isLikelyNetworkFetchError(err)) {
            throw err;
        }
        // Transport failure only (middleware never reached).
        await AppendRecordedAudioBase64(sessionId, toBase64(data));
        return "base64";
    }
}

/** Merge pending PCM byte chunks into one tight buffer and clear the list. */
function takePendingPCM(pending: Uint8Array[], pendingBytes: number): Uint8Array {
    if (pendingBytes <= 0 || pending.length === 0) return new Uint8Array(0);
    if (pending.length === 1 && pending[0].byteLength === pendingBytes) {
        const only = pending[0];
        pending.length = 0;
        return only;
    }
    const buf = new Uint8Array(pendingBytes);
    let off = 0;
    for (const p of pending) {
        buf.set(p, off);
        off += p.length;
    }
    pending.length = 0;
    return buf;
}

/**
 * Merge owned Int16 PCM slabs into one little-endian byte buffer (single copy).
 * Clears `pending` so capture can reuse the array.
 */
function takePendingInt16AsBytes(pending: Int16Array[], totalSamples: number): Uint8Array {
    if (totalSamples <= 0 || pending.length === 0) {
        pending.length = 0;
        return new Uint8Array(0);
    }
    if (pending.length === 1 && pending[0].length === totalSamples) {
        const only = pending[0];
        pending.length = 0;
        return new Uint8Array(only.buffer, only.byteOffset, only.byteLength);
    }
    const out = new Int16Array(totalSamples);
    let off = 0;
    for (const p of pending) {
        out.set(p, off);
        off += p.length;
    }
    pending.length = 0;
    return new Uint8Array(out.buffer, out.byteOffset, out.byteLength);
}

/** Drain upload promise chain without surfacing rejections (cancel / teardown). */
async function settleLiveUploadChain(chain: Promise<unknown>): Promise<void> {
    try {
        await chain;
    } catch {
        /* errors recorded on liveStreamErrorRef by the chain */
    }
}

/** Map backend/IPC errors to short user-facing text. */
function formatRecordingSaveError(err: unknown, zh: boolean): string {
    const raw = err instanceof Error ? err.message : String(err);
    const lower = raw.toLowerCase();
    if (lower.includes("chunk too large")) {
        return zh
            ? "录音分片过大，请重试；若反复失败请升级到最新版本"
            : "Recording chunk too large; please retry or update the app";
    }
    if (lower.includes("upload session") || lower.includes("session_id missing")) {
        return zh ? "录音上传会话失效，请重试" : "Recording upload session lost; please retry";
    }
    if (lower.includes("too many concurrent")) {
        return zh ? "同时进行的录音过多，请稍后再试" : "Too many concurrent recordings; try again later";
    }
    if (lower.includes("audio data too large") || lower.includes("upload size mismatch")) {
        return zh ? "录音过长或数据不完整，已超出上限" : "Recording exceeds the maximum size or is incomplete";
    }
    if (lower.includes("buffer overrun") || lower.includes("disk/ipc too slow")) {
        return zh
            ? "录音写入跟不上采集速度，请重试或关闭其他占用磁盘的程序"
            : "Recording could not keep up with capture; please retry";
    }
    // Mic permission only — do not match generic OS "access denied" on disk paths.
    if (
        lower.includes("notallowed") ||
        lower.includes("not allowed") ||
        lower.includes("permission dismissed") ||
        (lower.includes("microphone") && (lower.includes("permission") || lower.includes("denied")))
    ) {
        return zh ? "无法访问麦克风，请检查系统权限" : "Microphone permission denied";
    }
    return raw || (zh ? "保存录音失败" : "Failed to save recording");
}

/** Exported for unit tests. */
export const __recordAudioTestUtils = {
    encodeWAVFromInt16,
    encodeWAVFromChunks,
    toBase64,
    formatDuration,
    downsampleToInt16,
    mergeInt16Chunks,
    compactInt16Chunks,
    splitPcmForUpload,
    iteratePcmUploadSlabs,
    forEachPcmUploadSlab,
    appendPcmUploadSlabs,
    appendLivePCM,
    formatRecordingSaveError,
    COMPACT_CHUNK_THRESHOLD,
    COMPACT_MAX_HEAD_BYTES,
    UPLOAD_CHUNK_BYTES,
    LIVE_STREAM_FLUSH_BYTES,
    LIVE_MAX_QUEUED_FLUSHES,
    DEFAULT_LIVE_APPEND_PATH,
    isLikelyNetworkFetchError,
    takePendingPCM,
    takePendingInt16AsBytes,
};

function formatDuration(sec: number): string {
    const s = Math.max(0, Math.floor(sec));
    const h = Math.floor(s / 3600);
    const m = Math.floor((s % 3600) / 60);
    const r = s % 60;
    if (h > 0) {
        return `${h}:${String(m).padStart(2, "0")}:${String(r).padStart(2, "0")}`;
    }
    return `${m}:${String(r).padStart(2, "0")}`;
}

function mergeInt16Chunks(chunks: Int16Array[], totalSamples: number): Int16Array {
    const merged = new Int16Array(totalSamples);
    let off = 0;
    for (const c of chunks) {
        merged.set(c, off);
        off += c.length;
    }
    return merged;
}

export function RecordingSessionCard({
    title,
    purpose,
    theme: t,
    lang = "zh",
    active = true,
    onComplete,
}: RecordingSessionCardProps): React.ReactElement {
    const zh = lang.startsWith("zh");
    const [phase, setPhase] = useState<"starting" | "recording" | "paused" | "saving" | "done" | "error">(
        active ? "starting" : "done",
    );
    const [duration, setDuration] = useState(0);
    const [levels, setLevels] = useState<number[]>(() => Array(WAVEFORM_BARS).fill(0.08));
    const [error, setError] = useState<string | null>(null);

    const captureRef = useRef<RecordCaptureHandle | null>(null);
    /** Memory-fallback PCM slabs (unused when live disk stream is active). */
    const chunksRef = useRef<Int16Array[]>([]);
    const samplesRef = useRef(0);
    const pausedRef = useRef(false);
    const finishedRef = useRef(false);
    const savingRef = useRef(false);
    /** Accept residual PCM while capture.stop() flushes worklet tail. */
    const drainingRef = useRef(false);
    const startWallRef = useRef(0);
    const elapsedBeforePauseRef = useRef(0);
    const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);
    /** Live-to-disk session (preferred): PCM written during capture. */
    const liveSessionIdRef = useRef("");
    const liveAppendPathRef = useRef(DEFAULT_LIVE_APPEND_PATH);
    /** Owned Int16 slabs awaiting flush (no per-frame byte copy). */
    const livePendingPcmRef = useRef<Int16Array[]>([]);
    const livePendingSamplesRef = useRef(0);
    const liveUploadChainRef = useRef(Promise.resolve());
    const liveQueuedFlushesRef = useRef(0);
    const liveStreamErrorRef = useRef<Error | null>(null);
    const liveUsedBinaryRef = useRef(true);
    const liveFailOnceRef = useRef(false);
    const lastWaveformAtRef = useRef(0);
    const onCompleteRef = useRef(onComplete);
    onCompleteRef.current = onComplete;
    const titleRef = useRef(title);
    titleRef.current = title;
    const logDetailRef = useRef(false);
    const stopAndSaveRef = useRef<() => Promise<void>>(async () => {});
    const finishWithResultRef = useRef<(result: RecordingCompleteResult) => void>(() => {});

    useEffect(() => {
        const load = () => {
            LoadConfig()
                .then((cfg: any) => {
                    logDetailRef.current = !!(cfg?.log_detail_enabled ?? cfg?.LogDetailEnabled);
                })
                .catch(() => {
                    logDetailRef.current = false;
                });
        };
        load();
        const offChanged = EventsOn("config-changed", load);
        const offUpdated = EventsOn("config-updated", load);
        return () => {
            if (typeof offChanged === "function") offChanged();
            if (typeof offUpdated === "function") offUpdated();
        };
    }, []);

    const cleanupAudio = useCallback(async () => {
        if (timerRef.current) {
            clearInterval(timerRef.current);
            timerRef.current = null;
        }
        if (captureRef.current) {
            const cap = captureRef.current;
            captureRef.current = null;
            try {
                await Promise.resolve(cap.stop());
            } catch {
                /* ignore */
            }
        }
    }, []);

    const finishWithResult = useCallback(
        async (result: RecordingCompleteResult) => {
            if (finishedRef.current) return;
            finishedRef.current = true;
            savingRef.current = false;
            // Ensure capture fully stops (incl. worklet flush) before signaling complete.
            drainingRef.current = true;
            await cleanupAudio();
            drainingRef.current = false;
            setPhase(result.status === "error" ? "error" : "done");
            recordDebug(
                "session complete",
                {
                    status: result.status,
                    path: result.path,
                    durationSec: result.durationSec,
                    sizeBytes: result.sizeBytes,
                    error: result.error,
                },
                logDetailRef.current,
            );
            onCompleteRef.current(result);
        },
        [cleanupAudio],
    );
    finishWithResultRef.current = (r) => {
        void finishWithResult(r);
    };

    const markLiveStreamError = useCallback((err: unknown) => {
        if (liveStreamErrorRef.current) return;
        liveStreamErrorRef.current = err instanceof Error ? err : new Error(String(err));
        // Fail the card once (don't leave the user recording into a dead session).
        if (!liveFailOnceRef.current && !finishedRef.current && !savingRef.current) {
            liveFailOnceRef.current = true;
            void stopAndSaveRef.current();
        }
    }, []);

    const flushLivePending = useCallback(async () => {
        const samples = livePendingSamplesRef.current;
        if (samples <= 0) return;
        const buf = takePendingInt16AsBytes(livePendingPcmRef.current, samples);
        livePendingSamplesRef.current = 0;
        const sid = liveSessionIdRef.current;
        if (!sid || buf.length === 0) return;
        const mode = await appendLivePCM(sid, buf, liveAppendPathRef.current);
        if (mode === "base64") liveUsedBinaryRef.current = false;
    }, []);

    /** Enqueue owned Int16 PCM from downsample (no intermediate byte copy per frame). */
    const enqueueLivePCM = useCallback(
        (pcm: Int16Array) => {
            if (pcm.length === 0 || !liveSessionIdRef.current || liveStreamErrorRef.current) return;
            livePendingPcmRef.current.push(pcm);
            livePendingSamplesRef.current += pcm.length;
            const pendingBytes = livePendingSamplesRef.current * 2;
            if (pendingBytes < LIVE_STREAM_FLUSH_BYTES) return;

            // Backpressure: refuse unbounded queue if IPC/disk lags realtime.
            if (liveQueuedFlushesRef.current >= LIVE_MAX_QUEUED_FLUSHES) {
                markLiveStreamError(
                    new Error(
                        "recording buffer overrun (disk/IPC too slow); try a shorter take",
                    ),
                );
                return;
            }

            const samples = livePendingSamplesRef.current;
            const buf = takePendingInt16AsBytes(livePendingPcmRef.current, samples);
            livePendingSamplesRef.current = 0;
            liveQueuedFlushesRef.current += 1;
            liveUploadChainRef.current = liveUploadChainRef.current
                .then(async () => {
                    if (liveStreamErrorRef.current) return;
                    const sid = liveSessionIdRef.current;
                    if (!sid || buf.length === 0) return;
                    const mode = await appendLivePCM(sid, buf, liveAppendPathRef.current);
                    if (mode === "base64") liveUsedBinaryRef.current = false;
                })
                .catch((err: unknown) => {
                    markLiveStreamError(err);
                })
                .finally(() => {
                    liveQueuedFlushesRef.current = Math.max(0, liveQueuedFlushesRef.current - 1);
                });
        },
        [markLiveStreamError],
    );

    const abortLiveSession = useCallback(async (sessionId: string) => {
        // Let in-flight appends finish or fail before Cancel removes the session,
        // avoiding noisy "not found" races on the upload chain.
        await settleLiveUploadChain(liveUploadChainRef.current);
        livePendingPcmRef.current = [];
        livePendingSamplesRef.current = 0;
        try {
            await CancelRecordedAudioUpload(sessionId);
        } catch {
            /* ignore */
        }
    }, []);

    const stopAndSave = useCallback(async () => {
        if (finishedRef.current || savingRef.current) return;
        savingRef.current = true;
        setPhase("saving");
        pausedRef.current = true;
        // Drain residual worklet frames before we snapshot / final flush.
        drainingRef.current = true;
        await cleanupAudio();
        drainingRef.current = false;

        const totalSamples = samplesRef.current;
        const durationSec = totalSamples / TARGET_SAMPLE_RATE;
        const liveSid = liveSessionIdRef.current;
        recordDebug(
            "stop requested",
            {
                title: titleRef.current,
                samples: totalSamples,
                durationSec: Number(durationSec.toFixed(3)),
                chunks: chunksRef.current.length,
                live: !!liveSid,
            },
            logDetailRef.current,
        );

        // Prefer a real stream error over "too short" (e.g. fail in first 300ms).
        if (liveStreamErrorRef.current && liveSid) {
            const err = liveStreamErrorRef.current;
            liveSessionIdRef.current = "";
            await abortLiveSession(liveSid);
            const msg = formatRecordingSaveError(err, zh);
            setError(msg);
            finishWithResult({
                status: "error",
                title: titleRef.current,
                durationSec,
                error: msg,
            });
            return;
        }

        if (totalSamples < TARGET_SAMPLE_RATE * MIN_RECORD_SEC) {
            chunksRef.current = [];
            samplesRef.current = 0;
            livePendingPcmRef.current = [];
            livePendingSamplesRef.current = 0;
            if (liveSid) {
                liveSessionIdRef.current = "";
                await abortLiveSession(liveSid);
            }
            finishWithResult({
                status: "cancelled",
                title: titleRef.current,
                durationSec,
                error: zh ? "录音过短，已取消" : "Recording too short; cancelled",
            });
            return;
        }

        // ── Live path: PCM already (mostly) on disk ──────────────────────────
        if (liveSid) {
            let uploadSessionId = liveSid;
            try {
                if (liveStreamErrorRef.current) {
                    throw liveStreamErrorRef.current;
                }
                // Serialize: wait prior flushes, then drain tail pending.
                await settleLiveUploadChain(liveUploadChainRef.current);
                if (liveStreamErrorRef.current) {
                    throw liveStreamErrorRef.current;
                }
                await flushLivePending();
                const info = (await FinishRecordedAudioUpload(uploadSessionId)) as {
                    path?: string;
                    size_bytes?: number;
                    duration_sec?: number;
                    format?: string;
                    mp3_path?: string;
                    mp3_size_bytes?: number;
                    mp3_error?: string;
                };
                liveSessionIdRef.current = "";
                uploadSessionId = "";
                samplesRef.current = 0;
                chunksRef.current = [];
                recordDebug(
                    "live save result",
                    {
                        path: info?.path,
                        size_bytes: info?.size_bytes,
                        mp3_path: info?.mp3_path,
                        mp3_error: info?.mp3_error,
                        binary: liveUsedBinaryRef.current,
                    },
                    logDetailRef.current,
                );
                finishWithResult({
                    status: "stopped",
                    path: finishInfoString(info?.path) || "",
                    mp3Path: finishInfoString(info?.mp3_path),
                    mp3SizeBytes: typeof info?.mp3_size_bytes === "number" ? info.mp3_size_bytes : undefined,
                    mp3Error: finishInfoString(info?.mp3_error),
                    durationSec: typeof info?.duration_sec === "number" ? info.duration_sec : durationSec,
                    sizeBytes: typeof info?.size_bytes === "number" ? info.size_bytes : undefined,
                    format: finishInfoString(info?.format) || "wav",
                    title: titleRef.current,
                });
            } catch (err: unknown) {
                liveSessionIdRef.current = "";
                if (uploadSessionId) {
                    await abortLiveSession(uploadSessionId);
                }
                const raw = err instanceof Error ? err.message : String(err);
                const msg = formatRecordingSaveError(err, zh);
                recordDebug("live save failed", { error: raw, display: msg }, true);
                setError(msg);
                finishWithResult({
                    status: "error",
                    title: titleRef.current,
                    durationSec,
                    error: msg,
                });
            }
            return;
        }

        // ── Memory fallback: bulk upload on stop ─────────────────────────────
        const chunks = chunksRef.current;
        chunksRef.current = [];
        samplesRef.current = 0;

        let uploadSessionId = "";
        try {
            let actualSamples = 0;
            for (const c of chunks) {
                if (c && c.length > 0) actualSamples += c.length;
            }
            if (actualSamples <= 0) actualSamples = totalSamples;
            if (actualSamples <= 0) {
                throw new Error(zh ? "没有可保存的录音数据" : "No audio samples to save");
            }
            const dataBytes = actualSamples * 2;
            const wavBytes = 44 + dataBytes;
            const header = new ArrayBuffer(44);
            writeWavHeader(header, dataBytes, TARGET_SAMPLE_RATE);

            recordDebug(
                "saving wav (memory fallback header+pcm)",
                {
                    title: titleRef.current,
                    wavBytes,
                    pcmChunks: chunks.length,
                    samples: actualSamples,
                },
                logDetailRef.current,
            );

            const begin = (await BeginRecordedAudioUpload(titleRef.current)) as { session_id?: string };
            uploadSessionId = String(begin?.session_id || "").trim();
            if (!uploadSessionId) {
                throw new Error("upload session_id missing");
            }
            await AppendRecordedAudioBase64(uploadSessionId, toBase64(header));
            const slabCount = await appendPcmUploadSlabs(uploadSessionId, chunks, UPLOAD_CHUNK_BYTES);
            chunks.length = 0;
            recordDebug("pcm upload done", { slabs: slabCount, wavBytes }, logDetailRef.current);

            const info = (await FinishRecordedAudioUpload(uploadSessionId)) as {
                path?: string;
                size_bytes?: number;
                duration_sec?: number;
                format?: string;
                mp3_path?: string;
                mp3_size_bytes?: number;
                mp3_error?: string;
            };
            uploadSessionId = "";
            finishWithResult({
                status: "stopped",
                path: finishInfoString(info?.path) || "",
                mp3Path: finishInfoString(info?.mp3_path),
                mp3SizeBytes: typeof info?.mp3_size_bytes === "number" ? info.mp3_size_bytes : undefined,
                mp3Error: finishInfoString(info?.mp3_error),
                durationSec: typeof info?.duration_sec === "number" ? info.duration_sec : durationSec,
                sizeBytes: typeof info?.size_bytes === "number" ? info.size_bytes : wavBytes,
                format: finishInfoString(info?.format) || "wav",
                title: titleRef.current,
            });
        } catch (err: unknown) {
            if (uploadSessionId) {
                try {
                    await CancelRecordedAudioUpload(uploadSessionId);
                } catch {
                    /* ignore cancel errors */
                }
            }
            const raw = err instanceof Error ? err.message : String(err);
            const msg = formatRecordingSaveError(err, zh);
            recordDebug("save failed", { error: raw, display: msg }, true);
            setError(msg);
            finishWithResult({
                status: "error",
                title: titleRef.current,
                durationSec,
                error: msg,
            });
        }
    }, [abortLiveSession, cleanupAudio, finishWithResult, flushLivePending, zh]);

    stopAndSaveRef.current = stopAndSave;

    const togglePause = useCallback(() => {
        if (phase !== "recording" && phase !== "paused") return;
        if (phase === "recording") {
            pausedRef.current = true;
            elapsedBeforePauseRef.current += (Date.now() - startWallRef.current) / 1000;
            setPhase("paused");
            setLevels(Array(WAVEFORM_BARS).fill(0.06));
            recordDebug("paused", { elapsedSec: elapsedBeforePauseRef.current }, logDetailRef.current);
            return;
        }
        pausedRef.current = false;
        startWallRef.current = Date.now();
        setPhase("recording");
        recordDebug("resumed", undefined, logDetailRef.current);
    }, [phase]);

    useEffect(() => {
        if (!active || finishedRef.current) return;
        let cancelled = false;

        const ingestPCM = (input: Float32Array, sampleRate: number) => {
            if (finishedRef.current) return;
            // During normal pause/save, drop live frames — except residual flush from worklet stop.
            if ((pausedRef.current || savingRef.current) && !drainingRef.current) return;
            if (liveStreamErrorRef.current) return;
            const pcm = downsampleToInt16(input, sampleRate || TARGET_SAMPLE_RATE, TARGET_SAMPLE_RATE);
            samplesRef.current += pcm.length;

            if (liveSessionIdRef.current) {
                // Live-to-disk: hand off owned Int16 slabs (downsample allocates fresh).
                enqueueLivePCM(pcm);
            } else {
                chunksRef.current.push(pcm);
                if (chunksRef.current.length >= COMPACT_CHUNK_THRESHOLD) {
                    chunksRef.current = compactInt16Chunks(chunksRef.current);
                }
            }

            const now = Date.now();
            if (now - lastWaveformAtRef.current >= WAVEFORM_MIN_INTERVAL_MS) {
                lastWaveformAtRef.current = now;
                let sum = 0;
                const step = Math.max(1, Math.floor(pcm.length / 64));
                let n = 0;
                for (let i = 0; i < pcm.length; i += step) {
                    const f = pcm[i] / 32768;
                    sum += f * f;
                    n++;
                }
                const rms = Math.sqrt(sum / Math.max(1, n));
                const level = Math.min(1, rms * 12);
                setLevels((prev) => {
                    const next = prev.slice(1);
                    next.push(0.08 + level * 0.92);
                    return next;
                });
            }

            if (samplesRef.current / TARGET_SAMPLE_RATE >= MAX_DURATION_SEC) {
                void stopAndSaveRef.current();
            }
        };

        const start = async () => {
            try {
                // Prefer live disk session so long recordings never fill the JS heap.
                try {
                    const live = (await BeginLiveRecordedAudioUpload(titleRef.current)) as {
                        session_id?: string;
                        append_path?: string;
                    };
                    const sid = String(live?.session_id || "").trim();
                    if (sid) {
                        liveSessionIdRef.current = sid;
                        liveAppendPathRef.current = String(live?.append_path || DEFAULT_LIVE_APPEND_PATH);
                        liveStreamErrorRef.current = null;
                        liveFailOnceRef.current = false;
                        liveUsedBinaryRef.current = true;
                        livePendingPcmRef.current = [];
                        livePendingSamplesRef.current = 0;
                        liveQueuedFlushesRef.current = 0;
                        liveUploadChainRef.current = Promise.resolve();
                        recordDebug("live upload session", { session_id: sid }, logDetailRef.current);
                    }
                } catch (liveErr) {
                    recordDebug(
                        "live session unavailable; memory fallback",
                        { error: liveErr instanceof Error ? liveErr.message : String(liveErr) },
                        true,
                    );
                    liveSessionIdRef.current = "";
                }

                const handle = await startRecordCapture(
                    { onPCM: ingestPCM },
                    { sampleRate: TARGET_SAMPLE_RATE },
                );
                if (cancelled) {
                    handle.stop();
                    if (liveSessionIdRef.current) {
                        const sid = liveSessionIdRef.current;
                        liveSessionIdRef.current = "";
                        void CancelRecordedAudioUpload(sid);
                    }
                    return;
                }
                captureRef.current = handle;
                startWallRef.current = Date.now();
                elapsedBeforePauseRef.current = 0;
                setPhase("recording");
                recordDebug(
                    "mic opened",
                    {
                        title: titleRef.current,
                        purpose,
                        mode: handle.mode,
                        sampleRate: handle.sampleRate,
                        live: !!liveSessionIdRef.current,
                        targetSampleRate: TARGET_SAMPLE_RATE,
                    },
                    logDetailRef.current,
                );
                timerRef.current = setInterval(() => {
                    if (pausedRef.current || finishedRef.current) return;
                    const live = (Date.now() - startWallRef.current) / 1000;
                    setDuration(elapsedBeforePauseRef.current + live);
                }, 250);
            } catch (err: unknown) {
                const msg = err instanceof Error ? err.message : String(err);
                recordDebug("mic open failed", { error: msg }, true);
                setError(msg);
                finishWithResult({
                    status: "error",
                    title: titleRef.current,
                    error: msg,
                });
            }
        };

        void start();
        return () => {
            cancelled = true;
            if (finishedRef.current || savingRef.current) return;
            // Always resolve the backend pending session: save if usable, else cancel.
            // Silent cleanup alone would leave pendingRecordAudio hanging until TTL.
            if (samplesRef.current >= TARGET_SAMPLE_RATE * MIN_RECORD_SEC) {
                recordDebug("unmount flush save", { samples: samplesRef.current }, logDetailRef.current);
                void stopAndSaveRef.current();
                return;
            }
            recordDebug("unmount cancel", { samples: samplesRef.current }, logDetailRef.current);
            chunksRef.current = [];
            livePendingPcmRef.current = [];
            livePendingSamplesRef.current = 0;
            const dur = samplesRef.current / TARGET_SAMPLE_RATE;
            samplesRef.current = 0;
            if (liveSessionIdRef.current) {
                const sid = liveSessionIdRef.current;
                liveSessionIdRef.current = "";
                void abortLiveSession(sid);
            }
            finishWithResultRef.current({
                status: "cancelled",
                title: titleRef.current,
                durationSec: dur,
                error: zh ? "录音界面已关闭" : "Recording UI closed",
            });
        };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [active, enqueueLivePCM, abortLiveSession]);

    const statusLabel = (() => {
        switch (phase) {
            case "starting":
                return zh ? "正在打开麦克风…" : "Opening microphone…";
            case "recording":
                return zh ? "录音中" : "Recording";
            case "paused":
                return zh ? "已暂停" : "Paused";
            case "saving":
                return zh ? "正在保存…" : "Saving…";
            case "done":
                return zh ? "已结束" : "Finished";
            case "error":
                return zh ? "出错" : "Error";
            default:
                return "";
        }
    })();

    const isLive = phase === "recording" || phase === "paused" || phase === "starting";
    const recordControlsDisabled = phase === "starting";

    return (
        <div
            data-testid="recording-session-card"
            style={{
                margin: "8px 0 4px",
                padding: "12px 14px",
                borderRadius: 10,
                border: `1px solid ${t.fieldBorder}`,
                background: t.fieldBg,
                minWidth: 260,
                maxWidth: 420,
            }}
        >
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 8, marginBottom: 8 }}>
                <div style={{ minWidth: 0 }}>
                    <div style={{ fontWeight: 600, color: t.text, fontSize: 13, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                        {title || (zh ? "录音" : "Recording")}
                    </div>
                    {purpose ? (
                        <div style={{ color: t.textMuted, fontSize: 11, marginTop: 2 }}>{purpose}</div>
                    ) : null}
                </div>
                <div
                    style={{
                        display: "inline-flex",
                        alignItems: "center",
                        gap: 6,
                        color: phase === "recording" ? "#e74c3c" : t.textMuted,
                        fontSize: 12,
                        fontVariantNumeric: "tabular-nums",
                        flexShrink: 0,
                    }}
                >
                    {phase === "recording" ? (
                        <span
                            style={{
                                width: 8,
                                height: 8,
                                borderRadius: "50%",
                                background: "#e74c3c",
                                boxShadow: "0 0 0 0 rgba(231,76,60,0.5)",
                                animation: "recording-pulse 1.2s ease-out infinite",
                            }}
                        />
                    ) : null}
                    <span>{statusLabel}</span>
                    <span>{formatDuration(duration)}</span>
                </div>
            </div>

            <div
                data-testid="recording-waveform"
                aria-hidden
                style={{
                    display: "flex",
                    alignItems: "flex-end",
                    gap: 2,
                    height: 48,
                    marginBottom: 12,
                    padding: "0 2px",
                }}
            >
                {levels.map((lv, i) => (
                    <div
                        key={i}
                        style={{
                            flex: 1,
                            height: `${Math.max(8, Math.round(lv * 100))}%`,
                            borderRadius: 2,
                            background:
                                phase === "paused"
                                    ? t.textMuted
                                    : phase === "recording"
                                      ? "linear-gradient(180deg, #5dade2 0%, #2e86c1 100%)"
                                      : t.fieldBorder,
                            opacity: phase === "recording" ? 0.85 + lv * 0.15 : 0.45,
                            transition: "height 80ms linear",
                        }}
                    />
                ))}
            </div>

            {error ? (
                <div style={{ color: "#e74c3c", fontSize: 12, marginBottom: 8 }}>{error}</div>
            ) : null}

            {isLive ? (
                <div style={{ display: "flex", gap: 8 }}>
                    <button
                        type="button"
                        data-testid="recording-pause-btn"
                        disabled={recordControlsDisabled}
                        onClick={togglePause}
                        style={{
                            flex: 1,
                            padding: "8px 10px",
                            borderRadius: 8,
                            border: `1px solid ${t.fieldBorder}`,
                            background: t.fieldBg,
                            color: t.text,
                            cursor: phase === "starting" ? "not-allowed" : "pointer",
                            fontSize: 13,
                        }}
                    >
                        {phase === "paused" ? (zh ? "继续" : "Resume") : zh ? "暂停" : "Pause"}
                    </button>
                    <button
                        type="button"
                        data-testid="recording-stop-btn"
                        disabled={recordControlsDisabled}
                        onClick={() => void stopAndSave()}
                        style={{
                            flex: 1,
                            padding: "8px 10px",
                            borderRadius: 8,
                            border: "1px solid #c0392b",
                            background: "#e74c3c",
                            color: "#fff",
                            cursor: phase === "starting" ? "not-allowed" : "pointer",
                            fontSize: 13,
                            fontWeight: 600,
                        }}
                    >
                        {zh ? "停止" : "Stop"}
                    </button>
                </div>
            ) : phase === "saving" ? (
                <div style={{ color: t.textMuted, fontSize: 12 }}>
                    {zh ? "正在保存录音文件…" : "Saving recording…"}
                </div>
            ) : (
                <div style={{ color: t.textMuted, fontSize: 12 }}>
                    {zh ? "录音会话已结束" : "Recording session ended"}
                </div>
            )}

            <style>{`
                @keyframes recording-pulse {
                    0% { box-shadow: 0 0 0 0 rgba(231, 76, 60, 0.55); }
                    70% { box-shadow: 0 0 0 8px rgba(231, 76, 60, 0); }
                    100% { box-shadow: 0 0 0 0 rgba(231, 76, 60, 0); }
                }
            `}</style>
        </div>
    );
}

/**
 * Whether the composer should hard-lock for a live record_audio card.
 * Matches RecordingSessionCard activation: last assistant message in the
 * visible list, session still active, and not mid-stream.
 */
export function isRecordingInputLocked(
    messages: ReadonlyArray<{ role?: string; recordingSession?: { active?: boolean } | null | undefined }>,
    isStreaming: boolean,
): boolean {
    if (isStreaming) return false;
    for (let i = messages.length - 1; i >= 0; i--) {
        if (messages[i].role !== "assistant") continue;
        return !!messages[i].recordingSession?.active;
    }
    return false;
}

/** Format recording completion as a structured user message for the agent. */
export function formatRecordingCompletionMessage(result: RecordingCompleteResult): string {
    const lines = ["[Recording completed]"];
    lines.push(`status: ${result.status}`);
    if (result.title) lines.push(`title: ${result.title}`);
    if (result.path) lines.push(`path: ${result.path}`);
    if (result.mp3Path) lines.push(`mp3_path: ${result.mp3Path}`);
    if (result.mp3Error) lines.push(`mp3_error: ${result.mp3Error}`);
    if (typeof result.durationSec === "number") {
        lines.push(`duration_sec: ${result.durationSec.toFixed(1)}`);
        lines.push(`duration: ${formatDuration(result.durationSec)}`);
    }
    if (typeof result.sizeBytes === "number") lines.push(`size_bytes: ${result.sizeBytes}`);
    if (typeof result.mp3SizeBytes === "number") lines.push(`mp3_size_bytes: ${result.mp3SizeBytes}`);
    if (result.format) lines.push(`format: ${result.format}`);
    if (result.error) lines.push(`error: ${result.error}`);
    return lines.join("\n");
}

export function formatRecordingCompletionDisplay(result: RecordingCompleteResult, lang = "zh"): string {
    const zh = lang.startsWith("zh");
    if (result.status === "error") {
        return zh ? `录音失败：${result.error || "未知错误"}` : `Recording failed: ${result.error || "unknown error"}`;
    }
    if (result.status === "cancelled") {
        return zh ? "已取消录音（过短或未采集到有效音频）" : "Recording cancelled (too short or empty)";
    }
    const dur =
        typeof result.durationSec === "number" ? formatDuration(result.durationSec) : zh ? "未知" : "unknown";
    if (result.path) {
        if (result.mp3Path) {
            return zh
                ? `录音已保存（时长 ${dur}，已生成 MP3 存档）`
                : `Recording saved (duration ${dur}, MP3 archive ready)`;
        }
        return zh ? `录音已保存（时长 ${dur}）` : `Recording saved (duration ${dur})`;
    }
    return zh ? "录音已结束" : "Recording finished";
}
