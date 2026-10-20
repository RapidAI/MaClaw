import { ATTACHMENT_DOC_SHOT_HEIGHT, ATTACHMENT_DOC_SHOT_WIDTH } from "./attachmentOpeningPreview";

/**
 * CSS size of one whole page inside the shot box. The page keeps its aspect
 * ratio and both sides stay inside the box.
 */
export function pdfShotContainSize(pageWidth: number, pageHeight: number, maxWidth: number, maxHeight: number): { width: number; height: number } {
    if (!(pageWidth > 0) || !(pageHeight > 0) || !(maxWidth > 0) || !(maxHeight > 0)) {
        return { width: Math.max(0, maxWidth), height: Math.max(0, maxHeight) };
    }
    const scale = Math.min(maxWidth / pageWidth, maxHeight / pageHeight);
    return { width: pageWidth * scale, height: pageHeight * scale };
}

const SHOT_RASTER_MAX = 8;
const shotRasters = new Map<string, Promise<string>>();
let workerReady = false;

/** Keep the newest shot rasters. A long chat must not retain every page bitmap. */
export function retainShotRaster<T>(cache: Map<string, T>, key: string, value: T, max = SHOT_RASTER_MAX): T {
    cache.delete(key);
    cache.set(key, value);
    while (cache.size > max) {
        const oldest = cache.keys().next().value;
        if (oldest === undefined) break;
        cache.delete(oldest);
    }
    return value;
}

function pdfjsPublicUrl(path: string): string {
    const base = import.meta.env.BASE_URL || "./";
    const root = base.endsWith("/") ? base : `${base}/`;
    return new URL(`${root}pdfjs/${path}`, window.location.href).href;
}

async function loadPdfjs(): Promise<typeof import("pdfjs-dist")> {
    const pdfjs = await import("pdfjs-dist");
    if (!workerReady) {
        // Same-origin module worker. The dev server and the desktop build both
        // serve this file as JavaScript; pdf.js then starts it directly.
        pdfjs.GlobalWorkerOptions.workerSrc = pdfjsPublicUrl("pdf.worker.min.mjs");
        workerReady = true;
    }
    return pdfjs;
}

/**
 * Draw one PDF page so the whole page fits in the chat shot.
 *
 * The built-in PDF viewer will not zoom below 25%. At that floor a page is
 * already larger than the 160×120 shot, so the viewer crops it and draws
 * both scrollbars. A raster has no viewer chrome and can scale to the box.
 */
export function rasterizePdfShot(src: string, page: number, maxWidth = ATTACHMENT_DOC_SHOT_WIDTH, maxHeight = ATTACHMENT_DOC_SHOT_HEIGHT): Promise<string> {
    const preview = String(src || "").trim();
    if (!preview) return Promise.reject(new Error("empty pdf preview"));
    const pageNumber = page > 0 ? page : 1;
    const pixelRatio = Math.min(2, Math.max(1, window.devicePixelRatio || 1));
    const key = `${preview}\0${pageNumber}\0${maxWidth}x${maxHeight}\0${pixelRatio}`;
    const hit = shotRasters.get(key);
    if (hit) return retainShotRaster(shotRasters, key, hit);
    const pending = drawPdfShot(preview, pageNumber, maxWidth, maxHeight, pixelRatio).catch((error: unknown) => {
        if (shotRasters.get(key) === pending) shotRasters.delete(key);
        throw error;
    });
    return retainShotRaster(shotRasters, key, pending);
}

async function drawPdfShot(src: string, pageNumber: number, maxWidth: number, maxHeight: number, pixelRatio: number): Promise<string> {
    const response = await fetch(src);
    if (!response.ok) throw new Error(`pdf preview ${response.status}`);
    const data = new Uint8Array(await response.arrayBuffer());
    if (data.byteLength < 5) throw new Error("empty pdf preview");
    const pdfjs = await loadPdfjs();
    const task = pdfjs.getDocument({
        data,
        cMapUrl: pdfjsPublicUrl("cmaps/"),
        cMapPacked: true,
        standardFontDataUrl: pdfjsPublicUrl("standard_fonts/"),
        isEvalSupported: false,
        disableRange: true,
        disableStream: true,
        useSystemFonts: true,
    });
    const doc = await task.promise;
    try {
        const page = await doc.getPage(pageNumber);
        try {
            const base = page.getViewport({ scale: 1 });
            if (!(base.width > 0) || !(base.height > 0)) throw new Error("pdf page has no size");
            const fitted = pdfShotContainSize(base.width, base.height, maxWidth, maxHeight);
            const viewport = page.getViewport({ scale: (fitted.width / base.width) * pixelRatio });
            const canvas = document.createElement("canvas");
            canvas.width = Math.max(1, Math.round(viewport.width));
            canvas.height = Math.max(1, Math.round(viewport.height));
            const context = canvas.getContext("2d");
            if (!context) throw new Error("pdf preview canvas unavailable");
            context.fillStyle = "#ffffff";
            context.fillRect(0, 0, canvas.width, canvas.height);
            await page.render({ canvasContext: context, viewport, background: "#ffffff" }).promise;
            return canvas.toDataURL("image/png");
        } finally {
            page.cleanup();
        }
    } finally {
        await doc.destroy();
    }
}
