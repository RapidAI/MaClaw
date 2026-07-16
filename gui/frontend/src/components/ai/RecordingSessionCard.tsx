/**
 * Interactive long-form recording UI (waveform + pause/stop).
 * Used when the agent opens a record_audio session.
 *
 * Memory notes:
 * - Capture prefers AudioWorklet (ScriptProcessor fallback).
 * - PCM is stored as Int16 (half of Float32) in growing chunks.
 * - Waveform state updates are throttled to ~10 Hz to avoid React thrash.
 * - Upload streams header+PCM slabs (no full multi-hour WAV/base64 string).
 */
import React, { useCallback, useEffect, useRef, useState } from "react";
import {
    AppendRecordedAudioBase64,
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
/** Compact many small capture slabs into larger arrays (less GC pressure). */
const COMPACT_CHUNK_THRESHOLD = 48;
/** Binary chunk size for streaming upload (~256 KiB → ~350 KiB base64). */
const UPLOAD_CHUNK_BYTES = 256 * 1024;

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

/** Merge leading micro-chunks when the list grows large (long recordings). */
function compactInt16Chunks(chunks: Int16Array[]): Int16Array[] {
    if (chunks.length < COMPACT_CHUNK_THRESHOLD) return chunks;
    const keepTail = 8;
    const headCount = chunks.length - keepTail;
    if (headCount <= 1) return chunks;
    let headSamples = 0;
    for (let i = 0; i < headCount; i++) headSamples += chunks[i].length;
    const head = new Int16Array(headSamples);
    let off = 0;
    for (let i = 0; i < headCount; i++) {
        head.set(chunks[i], off);
        off += chunks[i].length;
    }
    return [head, ...chunks.slice(headCount)];
}

/**
 * Chunked base64 without spreading large typed arrays.
 * Uses apply on fixed-size windows (faster than per-byte string concat).
 */
function toBase64(buf: ArrayBuffer): string {
    const bytes = new Uint8Array(buf);
    const chunk = 0x8000;
    const parts: string[] = [];
    for (let i = 0; i < bytes.length; i += chunk) {
        const slice = bytes.subarray(i, Math.min(i + chunk, bytes.length));
        // Array.from is slower; manual push into apply-friendly array of codes.
        const codes = new Array(slice.length);
        for (let j = 0; j < slice.length; j++) codes[j] = slice[j];
        parts.push(String.fromCharCode.apply(null, codes));
    }
    return btoa(parts.join(""));
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
    COMPACT_CHUNK_THRESHOLD,
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

    const stopAndSave = useCallback(async () => {
        if (finishedRef.current || savingRef.current) return;
        savingRef.current = true;
        setPhase("saving");
        pausedRef.current = true;
        // Drain residual worklet frames before we snapshot chunks.
        drainingRef.current = true;
        await cleanupAudio();
        drainingRef.current = false;

        const totalSamples = samplesRef.current;
        const durationSec = totalSamples / TARGET_SAMPLE_RATE;
        recordDebug(
            "stop requested",
            {
                title: titleRef.current,
                samples: totalSamples,
                durationSec: Number(durationSec.toFixed(3)),
                chunks: chunksRef.current.length,
            },
            logDetailRef.current,
        );

        if (totalSamples < TARGET_SAMPLE_RATE * MIN_RECORD_SEC) {
            chunksRef.current = [];
            samplesRef.current = 0;
            finishWithResult({
                status: "cancelled",
                title: titleRef.current,
                durationSec,
                error: zh ? "录音过短，已取消" : "Recording too short; cancelled",
            });
            return;
        }

        const chunks = chunksRef.current;
        chunksRef.current = [];
        samplesRef.current = 0;

        let uploadSessionId = "";
        try {
            // Stream WAV as header + PCM slabs — never allocate a full multi-hour WAV buffer
            // or a full base64 string in the JS heap.
            let actualSamples = 0;
            for (const c of chunks) actualSamples += c.length;
            if (actualSamples <= 0) actualSamples = totalSamples;
            const dataBytes = actualSamples * 2;
            const wavBytes = 44 + dataBytes;
            const header = new ArrayBuffer(44);
            writeWavHeader(header, dataBytes, TARGET_SAMPLE_RATE);

            recordDebug(
                "saving wav (header+pcm stream)",
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

            // Upload PCM in groups of slabs so each IPC payload stays ~UPLOAD_CHUNK_BYTES.
            let pending: Uint8Array[] = [];
            let pendingBytes = 0;
            const flushPending = async () => {
                if (pendingBytes === 0) return;
                const buf = new Uint8Array(pendingBytes);
                let off = 0;
                for (const p of pending) {
                    buf.set(p, off);
                    off += p.length;
                }
                pending = [];
                pendingBytes = 0;
                // Use exact view slice — buf.buffer may be larger only if mis-created; here length matches.
                const ab = buf.byteOffset === 0 && buf.byteLength === buf.buffer.byteLength
                    ? buf.buffer
                    : buf.buffer.slice(buf.byteOffset, buf.byteOffset + buf.byteLength);
                await AppendRecordedAudioBase64(uploadSessionId, toBase64(ab));
            };
            for (const c of chunks) {
                const bytes = new Uint8Array(c.buffer, c.byteOffset, c.byteLength);
                pending.push(bytes);
                pendingBytes += bytes.length;
                if (pendingBytes >= UPLOAD_CHUNK_BYTES) {
                    await flushPending();
                }
            }
            await flushPending();
            // Release PCM slabs for GC ASAP.
            chunks.length = 0;

            const info = (await FinishRecordedAudioUpload(uploadSessionId)) as {
                path?: string;
                size_bytes?: number;
                duration_sec?: number;
                format?: string;
                meta_path?: string;
                debug_dump_path?: string;
                debug_dump_index?: string;
            };
            uploadSessionId = "";
            recordDebug(
                "save result",
                {
                    path: info?.path,
                    size_bytes: info?.size_bytes,
                    duration_sec: info?.duration_sec,
                    meta_path: info?.meta_path,
                    debug_dump_path: info?.debug_dump_path,
                    debug_dump_index: info?.debug_dump_index,
                },
                logDetailRef.current,
            );
            finishWithResult({
                status: "stopped",
                path: info?.path || "",
                durationSec: typeof info?.duration_sec === "number" ? info.duration_sec : durationSec,
                sizeBytes: typeof info?.size_bytes === "number" ? info.size_bytes : wavBytes,
                format: info?.format || "wav",
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
            const msg = err instanceof Error ? err.message : String(err);
            recordDebug("save failed", { error: msg }, true);
            setError(msg);
            finishWithResult({
                status: "error",
                title: titleRef.current,
                durationSec,
                error: msg,
            });
        }
    }, [cleanupAudio, finishWithResult, zh]);

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
            const pcm = downsampleToInt16(input, sampleRate || TARGET_SAMPLE_RATE, TARGET_SAMPLE_RATE);
            chunksRef.current.push(pcm);
            samplesRef.current += pcm.length;
            if (chunksRef.current.length >= COMPACT_CHUNK_THRESHOLD) {
                chunksRef.current = compactInt16Chunks(chunksRef.current);
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
                const handle = await startRecordCapture(
                    { onPCM: ingestPCM },
                    { sampleRate: TARGET_SAMPLE_RATE },
                );
                if (cancelled) {
                    handle.stop();
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
            const dur = samplesRef.current / TARGET_SAMPLE_RATE;
            samplesRef.current = 0;
            finishWithResultRef.current({
                status: "cancelled",
                title: titleRef.current,
                durationSec: dur,
                error: zh ? "录音界面已关闭" : "Recording UI closed",
            });
        };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [active]);

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
    if (typeof result.durationSec === "number") {
        lines.push(`duration_sec: ${result.durationSec.toFixed(1)}`);
        lines.push(`duration: ${formatDuration(result.durationSec)}`);
    }
    if (typeof result.sizeBytes === "number") lines.push(`size_bytes: ${result.sizeBytes}`);
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
        return zh ? `录音已保存（时长 ${dur}）` : `Recording saved (duration ${dur})`;
    }
    return zh ? "录音已结束" : "Recording finished";
}
