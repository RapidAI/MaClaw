/**
 * 33ms stream coalescing for the side-channel chat flows (/btw side
 * queries, degraded-turn "ai-continuation" recovery, VE stream chunks).
 * The main chat loop owns its round-based token buffer; these channels
 * only need a small shared helper with the same cadence:
 *
 * - The first delta of a burst applies immediately, so first paint keeps
 *   its latency (the thinking row / streaming bubble appears at once).
 * - Later deltas in the same burst buffer and flush as one update per
 *   ~33ms, so a fast SSE burst does not re-render React per event.
 * - Terminal events (stream_end / final / error / clear) flush eagerly,
 *   and dispose() flushes + stops timers on unmount.
 *
 * Flushes preserve delta order, and callers apply deltas through the
 * same functional-state updaters they used per event, so message order,
 * final text, and error rows stay byte-identical — only the update
 * frequency changes.
 */

export const STREAM_COALESCE_FLUSH_MS = 33;

export interface StreamCoalescer<K, T> {
    /**
     * Queue one delta under `key`. Returns true when the caller must apply
     * the delta immediately (first delta of a burst); false when the delta
     * was buffered for a later flush.
     */
    push(key: K, item: T): boolean;
    /** Apply buffered deltas for `key` in order and reset its burst state. */
    flush(key: K): void;
    /** Discard buffered deltas for `key` and reset its burst state. */
    drop(key: K): void;
    /** Flush every key and stop all timers (component unmount / resubscribe). */
    dispose(): void;
}

interface CoalesceBuffer<T> {
    items: T[];
    timer: ReturnType<typeof setTimeout> | null;
    /** The burst's first delta went straight to the caller already. */
    firstApplied: boolean;
}

export function createStreamCoalescer<K, T>(options: {
    onFlush: (key: K, items: T[]) => void;
    flushMs?: number;
}): StreamCoalescer<K, T> {
    const flushMs = options.flushMs ?? STREAM_COALESCE_FLUSH_MS;
    const buffers = new Map<K, CoalesceBuffer<T>>();

    const bufferFor = (key: K): CoalesceBuffer<T> => {
        let buffer = buffers.get(key);
        if (!buffer) {
            buffer = { items: [], timer: null, firstApplied: false };
            buffers.set(key, buffer);
        }
        return buffer;
    };

    const flushBuffer = (key: K, buffer: CoalesceBuffer<T>) => {
        if (buffer.timer) {
            clearTimeout(buffer.timer);
            buffer.timer = null;
        }
        if (buffer.items.length > 0) {
            const items = buffer.items;
            buffer.items = [];
            options.onFlush(key, items);
        }
        buffer.firstApplied = false;
    };

    return {
        push(key, item) {
            const buffer = bufferFor(key);
            if (!buffer.firstApplied) {
                buffer.firstApplied = true;
                return true;
            }
            buffer.items.push(item);
            if (!buffer.timer) {
                buffer.timer = setTimeout(() => flushBuffer(key, buffer), flushMs);
            }
            return false;
        },
        flush(key) {
            const buffer = buffers.get(key);
            if (buffer) flushBuffer(key, buffer);
        },
        drop(key) {
            const buffer = buffers.get(key);
            if (!buffer) return;
            if (buffer.timer) {
                clearTimeout(buffer.timer);
                buffer.timer = null;
            }
            buffer.items = [];
            buffer.firstApplied = false;
        },
        dispose() {
            for (const [key, buffer] of Array.from(buffers.entries())) {
                flushBuffer(key, buffer);
            }
            buffers.clear();
        },
    };
}
