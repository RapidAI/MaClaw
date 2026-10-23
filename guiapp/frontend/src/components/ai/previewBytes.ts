/** Matches the in-app document preview ceiling. A larger Content-Length is not allocated up front. */
const PREVIEW_BODY_ALLOC_LIMIT = 512 << 20;
const PREVIEW_PROGRESS_INTERVAL_MS = 100;

export function previewContentLength(response: { headers?: { get?: (name: string) => string | null } }): number | null {
    const get = response.headers?.get;
    if (typeof get !== "function") return null;
    const raw = get.call(response.headers, "content-length");
    if (raw == null || String(raw).trim() === "") return null;
    const value = Number(raw);
    if (!Number.isFinite(value) || value <= 0) return null;
    return value;
}

export function isPreviewAbortError(err: unknown): boolean {
    return !!err && typeof err === "object" && (err as { name?: string }).name === "AbortError";
}

function throwIfAborted(signal: AbortSignal | undefined) {
    if (signal?.aborted) throw new DOMException("The operation was aborted.", "AbortError");
}

type PreviewReader = {
    read: () => Promise<{ done?: boolean; value?: Uint8Array }>;
    cancel?: () => Promise<void> | void;
};

/**
 * Copy a preview body and report received/total while it arrives.
 *
 * The copy happens before the next read. A stream may reuse one Uint8Array
 * for every chunk. A known Content-Length within the document cap is
 * allocated once. Missing length stays indeterminate.
 */
export async function readPreviewBody(
    response: Response,
    options: { signal?: AbortSignal; onProgress?: (ratio: number | null) => void } = {},
): Promise<Uint8Array> {
    const signal = options.signal;
    const onProgress = options.onProgress;
    throwIfAborted(signal);
    const declared = previewContentLength(response);
    const body = response.body as { getReader?: () => PreviewReader } | null;
    if (!body || typeof body.getReader !== "function") {
        const buffered = new Uint8Array(await response.arrayBuffer());
        throwIfAborted(signal);
        if (onProgress) {
            if (declared != null) onProgress(Math.min(1, buffered.byteLength / declared));
            else onProgress(null);
        }
        return buffered;
    }
    const reader = body.getReader();
    let data = new Uint8Array(declared != null && declared <= PREVIEW_BODY_ALLOC_LIMIT ? declared : 0);
    let offset = 0;
    let lastEmit = 0;
    let lastPct = -1;
    const emit = (force: boolean) => {
        if (!onProgress || declared == null) return;
        const ratio = Math.min(1, offset / declared);
        const pct = Math.round(ratio * 100);
        const now = Date.now();
        if (!force && pct === lastPct) return;
        if (!force && pct < 100 && now - lastEmit < PREVIEW_PROGRESS_INTERVAL_MS) return;
        lastPct = pct;
        lastEmit = now;
        onProgress(ratio);
    };
    const onAbort = () => {
        void Promise.resolve(reader.cancel?.()).catch(() => undefined);
    };
    signal?.addEventListener("abort", onAbort);
    try {
        if (declared == null) onProgress?.(null);
        for (;;) {
            throwIfAborted(signal);
            const step = await reader.read();
            if (step.done) break;
            const value = step.value;
            if (!value?.byteLength) continue;
            const next = offset + value.byteLength;
            if (next > data.byteLength) {
                let cap = data.byteLength > 0 ? data.byteLength : value.byteLength;
                while (cap < next) cap *= 2;
                const grown = new Uint8Array(cap);
                if (offset > 0) grown.set(data.subarray(0, offset), 0);
                data = grown;
            }
            data.set(value, offset);
            offset = next;
            emit(false);
        }
    } finally {
        signal?.removeEventListener("abort", onAbort);
        await Promise.resolve(reader.cancel?.()).catch(() => undefined);
    }
    throwIfAborted(signal);
    emit(true);
    if (offset === data.byteLength) return data;
    return data.slice(0, offset);
}
