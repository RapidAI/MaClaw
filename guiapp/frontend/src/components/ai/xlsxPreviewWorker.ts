import { sheetGridFromWire, type SheetGrid, type WireSheetGrid } from "./xlsxPreviewParse";

export type XlsxPreviewParser = {
    parse: (bytes: Uint8Array) => Promise<{ names: string[]; grids: SheetGrid[] }>;
    stop: () => void;
};

function abortError(): DOMException {
    return new DOMException("The operation was aborted.", "AbortError");
}

function transferableBuffer(bytes: Uint8Array): ArrayBuffer {
    if (bytes.byteOffset === 0 && bytes.byteLength === bytes.buffer.byteLength) return bytes.buffer as ArrayBuffer;
    return bytes.slice().buffer;
}

function gridsFromMessage(data: { names?: unknown; grids?: unknown }): { names: string[]; grids: SheetGrid[] } {
    const names = Array.isArray(data.names) ? data.names.map((name) => String(name ?? "")) : [];
    const grids = Array.isArray(data.grids) ? data.grids.map((grid) => sheetGridFromWire(grid as WireSheetGrid)) : [];
    if (names.length !== grids.length) throw new Error("spreadsheet parser returned an incomplete preview");
    return { names, grids };
}

/**
 * Parse a workbook off the UI thread. The bytes stay on this thread until the
 * worker has loaded and posted ready; a worker that fails to start leaves
 * them here for the main-thread parser. stop() ends a parse the reader left.
 */
export function startXlsxPreviewParser(): XlsxPreviewParser {
    const worker = new Worker(new URL("./xlsxPreview.worker.ts", import.meta.url), { type: "module", name: "xlsx-preview" });
    let stopped = false;
    let readySettled = false;
    let rejectActive: ((err: unknown) => void) | null = null;
    let finishReady: (err?: unknown) => void = () => {};
    const ready = new Promise<void>((resolve, reject) => {
        const timer = window.setTimeout(() => finishReady(new Error("spreadsheet parser did not start")), 8000);
        finishReady = (err?: unknown) => {
            if (readySettled) return;
            readySettled = true;
            window.clearTimeout(timer);
            if (err) reject(err);
            else resolve();
        };
        worker.addEventListener("message", (event: MessageEvent<{ type?: string }>) => {
            if (event.data?.type === "ready") finishReady();
        });
    });
    worker.addEventListener("error", (event) => {
        const err = new Error(event.message || "spreadsheet parser failed");
        if (!readySettled) finishReady(err);
        else rejectActive?.(err);
    });
    return {
        stop() {
            if (stopped) return;
            stopped = true;
            finishReady(abortError());
            rejectActive?.(abortError());
            rejectActive = null;
            worker.terminate();
        },
        async parse(bytes: Uint8Array) {
            await ready;
            if (stopped) throw abortError();
            const buffer = transferableBuffer(bytes);
            return await new Promise((resolve, reject) => {
                rejectActive = reject;
                const onMessage = (event: MessageEvent<{ type?: string; message?: string; names?: unknown; grids?: unknown }>) => {
                    const data = event.data;
                    if (!data || (data.type !== "preview" && data.type !== "error")) return;
                    worker.removeEventListener("message", onMessage);
                    rejectActive = null;
                    if (data.type === "error") {
                        reject(new Error(data.message || "spreadsheet parser failed"));
                        return;
                    }
                    try {
                        resolve(gridsFromMessage(data));
                    } catch (err) {
                        reject(err);
                    }
                };
                worker.addEventListener("message", onMessage);
                try {
                    worker.postMessage({ type: "parse", bytes: buffer }, [buffer]);
                } catch (err) {
                    worker.removeEventListener("message", onMessage);
                    rejectActive = null;
                    reject(err);
                }
            });
        },
    };
}
