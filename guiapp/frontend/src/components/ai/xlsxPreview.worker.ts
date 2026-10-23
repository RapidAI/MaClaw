import * as XLSX from "xlsx";
import { previewWorkbookGrids, readPreviewWorkbook, releasePreviewWorkbook, wireSheetGrid } from "./xlsxPreviewParse";

type WorkerHost = {
    postMessage: (message: unknown, transfer?: Transferable[]) => void;
    onmessage: ((event: MessageEvent<{ type?: string; bytes?: ArrayBuffer }>) => void) | null;
};

function install(host: WorkerHost) {
    host.postMessage({ type: "ready" });
    host.onmessage = (event) => {
        const data = event.data;
        if (!data || data.type !== "parse" || !(data.bytes instanceof ArrayBuffer)) return;
        let wb: import("xlsx").WorkBook | null = null;
        try {
            wb = readPreviewWorkbook(XLSX as unknown as Parameters<typeof readPreviewWorkbook>[0], new Uint8Array(data.bytes));
            const preview = previewWorkbookGrids(XLSX as unknown as Parameters<typeof previewWorkbookGrids>[0], wb);
            host.postMessage({
                type: "preview",
                names: preview.names,
                grids: preview.grids.map((grid) => wireSheetGrid(grid)),
            });
        } catch (err) {
            const message = err instanceof Error ? err.message : String(err || "");
            host.postMessage({ type: "error", message });
        } finally {
            if (wb) releasePreviewWorkbook(wb);
        }
    };
}

// The page bundle must not post into the window if this module is evaluated
// there. A dedicated worker is an instance of DedicatedWorkerGlobalScope.
const scopeCtor = (globalThis as { DedicatedWorkerGlobalScope?: new () => object }).DedicatedWorkerGlobalScope;
if (typeof scopeCtor === "function" && globalThis instanceof scopeCtor && typeof (globalThis as unknown as WorkerHost).postMessage === "function") {
    install(globalThis as unknown as WorkerHost);
}
