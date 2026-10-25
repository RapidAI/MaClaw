import { renderMermaidFenceToPNGDataURL } from "./AssistantMermaidDiagram";
import { lightTheme } from "./aiAssistantPanelTheme";

// The PDF renderer asks the window to draw one fence, then waits for the PNG.
export const MERMAID_PDF_RASTER_EVENT = "maclaw:mermaid-raster";

export type MermaidPdfRasterRequest = {
    id?: string;
    ID?: string;
    source?: string;
    Source?: string;
};

type GoApp = {
    MermaidRasterReady?: () => Promise<void> | void;
    SubmitMermaidRaster?: (id: string, dataURL: string) => Promise<void> | void;
};

function goApp(): GoApp | null {
    const w = window as unknown as { go?: { main?: { App?: GoApp }; guiapp?: { App?: GoApp } } };
    return w.go?.main?.App || w.go?.guiapp?.App || null;
}

export function mermaidPdfRasterRequest(payload: MermaidPdfRasterRequest | null | undefined): { id: string; source: string } | null {
    const id = String(payload?.id ?? payload?.ID ?? "").trim();
    if (!id) return null;
    return { id, source: String(payload?.source ?? payload?.Source ?? "") };
}

/**
 * Draw one fence and hand the PNG back to the PDF renderer.
 *
 * A failure is an empty picture. The renderer then keeps the fence as source,
 * which is what the page would have shown when the diagram itself would not draw.
 */
export async function deliverMermaidPdfRaster(
    payload: MermaidPdfRasterRequest | null | undefined,
    render: (source: string) => Promise<string | null>,
    submit: (id: string, dataURL: string) => void,
): Promise<void> {
    const request = mermaidPdfRasterRequest(payload);
    if (!request) return;
    let dataURL = "";
    try {
        dataURL = (await render(request.source)) || "";
    } catch {
        dataURL = "";
    }
    submit(request.id, dataURL);
}

function runtimeEventsOn(): ((eventName: string, callback: (...data: unknown[]) => void) => void) | null {
    const runtime = (window as unknown as { runtime?: { EventsOn?: (eventName: string, callback: (...data: unknown[]) => void) => void } }).runtime;
    return typeof runtime?.EventsOn === "function" ? runtime.EventsOn.bind(runtime) : null;
}

/**
 * Listen for PDF diagram requests for the life of the window.
 *
 * The PDF is built in Go, which cannot run mermaid. The same drawer the chat
 * uses paints the fence onto a white page and returns the PNG. Installed once,
 * including when the runtime appears a moment after this module loads.
 */
export function installMermaidPdfRaster(): void {
    const flag = window as unknown as { __maclawMermaidPdfRaster?: boolean; __maclawMermaidPdfRasterWait?: boolean };
    if (flag.__maclawMermaidPdfRaster) return;
    const eventsOn = runtimeEventsOn();
    if (!eventsOn) {
        if (flag.__maclawMermaidPdfRasterWait) return;
        flag.__maclawMermaidPdfRasterWait = true;
        let tries = 0;
        const timer = window.setInterval(() => {
            tries += 1;
            if (runtimeEventsOn() || tries > 50) {
                window.clearInterval(timer);
                flag.__maclawMermaidPdfRasterWait = false;
                if (runtimeEventsOn()) installMermaidPdfRaster();
            }
        }, 100);
        return;
    }
    flag.__maclawMermaidPdfRaster = true;
    try {
        eventsOn(MERMAID_PDF_RASTER_EVENT, (payload: unknown) => {
            void deliverMermaidPdfRaster(
                payload as MermaidPdfRasterRequest,
                (source) => renderMermaidFenceToPNGDataURL(source, lightTheme),
                (id, dataURL) => {
                    void goApp()?.SubmitMermaidRaster?.(id, dataURL);
                },
            );
        });
    } catch {
        flag.__maclawMermaidPdfRaster = false;
        return;
    }
    announceMermaidPdfRasterReady();
}

function announceMermaidPdfRasterReady(tries = 0): void {
    const ready = goApp()?.MermaidRasterReady;
    if (typeof ready === "function") {
        // The binding returns a promise. A rejection means the call never
        // reached the PDF renderer, so dropping it would leave every later
        // diagram as source for this page.
        Promise.resolve()
            .then(() => ready())
            .catch(() => scheduleMermaidRasterReady(tries));
        return;
    }
    scheduleMermaidRasterReady(tries);
}

function scheduleMermaidRasterReady(tries: number): void {
    // A restored task can ask for a PDF while the page is still starting.
    // Keep announcing until the binding answers, then stop.
    if (tries >= 120) return;
    window.setTimeout(() => announceMermaidPdfRasterReady(tries + 1), 250);
}
