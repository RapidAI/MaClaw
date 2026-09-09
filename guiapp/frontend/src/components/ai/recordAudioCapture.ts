/**
 * Microphone capture for long-form recording.
 * Prefers AudioWorklet (modern, non-deprecated); falls back to ScriptProcessor.
 */

export type RecordCaptureMode = "worklet" | "script";

export type RecordCaptureHandlers = {
    /** Float32 mono frames at AudioContext sampleRate. */
    onPCM: (samples: Float32Array, sampleRate: number) => void;
};

export type RecordCaptureHandle = {
    mode: RecordCaptureMode;
    context: AudioContext;
    stream: MediaStream;
    sampleRate: number;
    /** Stops capture; for worklet mode flushes residual frames before teardown. */
    stop: () => void | Promise<void>;
};

const WORKLET_NAME = "record-pcm-processor";

// Batches ~4096 frames before posting to main thread (similar to ScriptProcessor quantum).
// Supports {type:'flush'} so stop() can emit the tail buffer (<4096 frames).
const WORKLET_SOURCE = `
class RecordPCMProcessor extends AudioWorkletProcessor {
  constructor() {
    super();
    this._parts = [];
    this._count = 0;
    this._target = 4096;
    this.port.onmessage = (ev) => {
      if (ev.data && ev.data.type === "flush") {
        this._flush(true);
      }
    };
  }
  _flush(ack) {
    if (this._count > 0) {
      const out = new Float32Array(this._count);
      let off = 0;
      for (let i = 0; i < this._parts.length; i++) {
        out.set(this._parts[i], off);
        off += this._parts[i].length;
      }
      this._parts = [];
      this._count = 0;
      this.port.postMessage(out, [out.buffer]);
    }
    if (ack) {
      this.port.postMessage({ type: "flushed" });
    }
  }
  process(inputs) {
    const ch = inputs[0] && inputs[0][0];
    if (!ch || ch.length === 0) return true;
    // Copy — input buffers are reused by the audio engine.
    const copy = new Float32Array(ch.length);
    copy.set(ch);
    this._parts.push(copy);
    this._count += copy.length;
    if (this._count >= this._target) {
      this._flush(false);
    }
    return true;
  }
}
registerProcessor("${WORKLET_NAME}", RecordPCMProcessor);
`;

let workletBlobURL: string | null = null;

function getWorkletURL(): string {
    if (!workletBlobURL) {
        workletBlobURL = URL.createObjectURL(new Blob([WORKLET_SOURCE], { type: "application/javascript" }));
    }
    return workletBlobURL;
}

function supportsAudioWorklet(ctx: AudioContext): boolean {
    return typeof (ctx as any).audioWorklet?.addModule === "function";
}

/**
 * Open mic and start capture. Caller owns stream/context lifetime via handle.stop().
 */
export async function startRecordCapture(
    handlers: RecordCaptureHandlers,
    opts?: { sampleRate?: number; echoCancellation?: boolean; noiseSuppression?: boolean },
): Promise<RecordCaptureHandle> {
    const targetRate = opts?.sampleRate ?? 16000;
    const stream = await navigator.mediaDevices.getUserMedia({
        audio: {
            echoCancellation: opts?.echoCancellation !== false,
            noiseSuppression: opts?.noiseSuppression !== false,
            channelCount: 1,
            sampleRate: { ideal: targetRate },
        },
    });

    const ctx = new AudioContext({ sampleRate: targetRate });
    if (ctx.state === "suspended") {
        await ctx.resume().catch(() => {});
    }
    const source = ctx.createMediaStreamSource(stream);
    const silent = ctx.createGain();
    silent.gain.value = 0;

    let mode: RecordCaptureMode = "script";
    let workletNode: AudioWorkletNode | null = null;
    let scriptNode: ScriptProcessorNode | null = null;

    if (supportsAudioWorklet(ctx)) {
        try {
            await ctx.audioWorklet.addModule(getWorkletURL());
            workletNode = new AudioWorkletNode(ctx, WORKLET_NAME, {
                numberOfInputs: 1,
                numberOfOutputs: 1,
                channelCount: 1,
            });
            workletNode.port.onmessage = (ev: MessageEvent) => {
                const data = ev.data;
                if (data instanceof Float32Array && data.length > 0) {
                    handlers.onPCM(data, ctx.sampleRate || targetRate);
                }
            };
            source.connect(workletNode);
            // Keep graph alive without audible monitoring.
            workletNode.connect(silent);
            silent.connect(ctx.destination);
            mode = "worklet";
        } catch {
            try {
                workletNode?.disconnect();
            } catch {
                /* ignore */
            }
            try {
                // Clear partial graph so script fallback starts clean.
                source.disconnect();
            } catch {
                /* ignore */
            }
            try {
                silent.disconnect();
            } catch {
                /* ignore */
            }
            workletNode = null;
            mode = "script";
        }
    }

    if (mode === "script") {
        scriptNode = ctx.createScriptProcessor(4096, 1, 1);
        scriptNode.onaudioprocess = (ev) => {
            const input = ev.inputBuffer.getChannelData(0);
            // Copy — channel data is reused.
            const copy = new Float32Array(input.length);
            copy.set(input);
            handlers.onPCM(copy, ctx.sampleRate || targetRate);
        };
        source.connect(scriptNode);
        scriptNode.connect(silent);
        silent.connect(ctx.destination);
    }

    let stopped = false;
    const stop = (): Promise<void> => {
        if (stopped) return Promise.resolve();
        stopped = true;

        const disconnectAll = () => {
            try {
                workletNode?.port.close();
            } catch {
                /* ignore */
            }
            try {
                workletNode?.disconnect();
            } catch {
                /* ignore */
            }
            try {
                scriptNode?.disconnect();
            } catch {
                /* ignore */
            }
            try {
                source.disconnect();
            } catch {
                /* ignore */
            }
            try {
                silent.disconnect();
            } catch {
                /* ignore */
            }
            try {
                void ctx.close();
            } catch {
                /* ignore */
            }
            for (const track of stream.getTracks()) {
                try {
                    track.stop();
                } catch {
                    /* ignore */
                }
            }
        };

        // Flush worklet tail buffer so we don't drop up to ~256ms of audio on stop.
        // Wait until residual PCM is delivered to onPCM before tearing down.
        if (mode === "worklet" && workletNode) {
            return new Promise<void>((resolve) => {
                let settled = false;
                const finish = () => {
                    if (settled) return;
                    settled = true;
                    disconnectAll();
                    resolve();
                };
                const onMsg = (ev: MessageEvent) => {
                    if (ev.data && (ev.data as { type?: string }).type === "flushed") {
                        try {
                            workletNode?.port.removeEventListener("message", onMsg as EventListener);
                        } catch {
                            /* ignore */
                        }
                        // Let the residual Float32 message run first (same turn ordering:
                        // flush posts PCM then {flushed}; both are queued).
                        queueMicrotask(finish);
                    }
                };
                try {
                    workletNode.port.addEventListener("message", onMsg as EventListener);
                    workletNode.port.postMessage({ type: "flush" });
                } catch {
                    finish();
                    return;
                }
                // Hard cap so stop() never hangs if the worklet is already torn down.
                setTimeout(finish, 100);
            });
        }

        disconnectAll();
        return Promise.resolve();
    };

    return {
        mode,
        context: ctx,
        stream,
        sampleRate: ctx.sampleRate || targetRate,
        stop,
    };
}
