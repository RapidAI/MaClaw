// Marks on the bug-report screenshot. Coordinates are 0..1 across the
// image so the on-screen preview and the exported PNG stay aligned when
// the dialog scales the picture.

export const BUG_REPORT_MARK_COLORS = [
    { id: "yellow", value: "#F5C518" },
    { id: "red", value: "#E5484D" },
    { id: "blue", value: "#3B82F6" },
    { id: "green", value: "#1F9D55" },
    { id: "black", value: "#1C2430" },
] as const;

export const BUG_REPORT_HIGHLIGHT_ALPHA = 0.42;
export const BUG_REPORT_MARK_POINT_LIMIT = 4000;
/** Long edge of the flattened PNG. A full 8K canvas is large enough to stall the webview. */
export const BUG_REPORT_EXPORT_MAX_EDGE = 4096;

const MIN_POINT_STEP = 0.0015;
const MIN_BOX_SPAN = 0.004;

export type BugReportMarkTool = "highlight" | "pen" | "box";

export interface BugReportPoint {
    x: number;
    y: number;
}

export interface BugReportMark {
    tool: BugReportMarkTool;
    color: string;
    points: BugReportPoint[];
}

export interface BugReportShotExportResult {
    annotated: string;
    failed: boolean;
}

export type BugReportShotExporter = () => BugReportShotExportResult;

export function emptyBugReportShotExport(): BugReportShotExportResult {
    return { annotated: "", failed: false };
}

export interface BugReportDrawSurface {
    save(): void;
    restore(): void;
    beginPath(): void;
    moveTo(x: number, y: number): void;
    lineTo(x: number, y: number): void;
    stroke(): void;
    fill(): void;
    arc(x: number, y: number, radius: number, startAngle: number, endAngle: number): void;
    strokeRect(x: number, y: number, w: number, h: number): void;
    strokeStyle: string | CanvasGradient | CanvasPattern;
    fillStyle: string | CanvasGradient | CanvasPattern;
    lineWidth: number;
    lineCap: CanvasLineCap;
    lineJoin: CanvasLineJoin;
    globalAlpha: number;
}

export function bugReportMarkColor(value: string): string {
    return BUG_REPORT_MARK_COLORS.some((item) => item.value === value) ? value : BUG_REPORT_MARK_COLORS[0].value;
}

// The load event is not the bitmap. A data URL that finished before the
// listener was attached is already complete, and waiting for another load
// leaves the overlay at its initial size.
export function bugReportImageReady(image: { complete: boolean; naturalWidth: number } | null | undefined): boolean {
    return !!image && image.complete && image.naturalWidth > 0;
}

export function bugReportPointerPoint(
    rect: { left: number; top: number; width: number; height: number },
    clientX: number,
    clientY: number,
): BugReportPoint | null {
    if (rect.width <= 0 || rect.height <= 0) return null;
    return {
        x: clamp01((clientX - rect.left) / rect.width),
        y: clamp01((clientY - rect.top) / rect.height),
    };
}

// A pointermove on Windows is often one event for a whole sweep. The
// browser keeps the intermediate positions on the event; the event's own
// client point is only the newest. An empty history means this host did
// not coalesce, so the event itself is the sample.
export function bugReportPointerSamples(
    event: { clientX: number; clientY: number; getCoalescedEvents?: () => ReadonlyArray<{ clientX: number; clientY: number }> },
): Array<{ clientX: number; clientY: number }> {
    const samples = event.getCoalescedEvents?.();
    const source = samples && samples.length > 0 ? samples : [event];
    return source.map((sample) => ({ clientX: sample.clientX, clientY: sample.clientY }));
}

// Mutates the in-progress stroke. A committed mark is copied first by
// appendBugReportPoint so a finished stroke stays put.
export function extendBugReportMark(mark: BugReportMark, point: BugReportPoint): boolean {
    if (mark.tool === "box") {
        const origin = mark.points[0] ?? point;
        const prev = mark.points[1];
        if (prev && mark.points[0] === origin && prev.x === point.x && prev.y === point.y) return false;
        mark.points = [origin, point];
        return true;
    }
    if (mark.points.length >= BUG_REPORT_MARK_POINT_LIMIT) return false;
    const last = mark.points[mark.points.length - 1];
    if (last && Math.hypot(last.x - point.x, last.y - point.y) < MIN_POINT_STEP) return false;
    mark.points.push(point);
    return true;
}

export function appendBugReportPoint(mark: BugReportMark, point: BugReportPoint): BugReportMark {
    const next: BugReportMark = { tool: mark.tool, color: mark.color, points: mark.points.slice() };
    extendBugReportMark(next, point);
    return next;
}

export function bugReportMarkWorthKeeping(mark: BugReportMark): boolean {
    if (mark.tool === "box") {
        if (mark.points.length < 2) return false;
        const spanX = Math.abs(mark.points[1].x - mark.points[0].x);
        const spanY = Math.abs(mark.points[1].y - mark.points[0].y);
        return spanX >= MIN_BOX_SPAN || spanY >= MIN_BOX_SPAN;
    }
    return mark.points.length > 0;
}

// A fraction of the picture width, so a mark on the scaled preview has the
// same thickness on the flattened PNG. No pixel floor: a floor makes a
// narrow preview stroke fatter than the exported one.
export function bugReportStrokeWidth(tool: BugReportMarkTool, width: number): number {
    const ratio = tool === "highlight" ? 0.02 : 0.003;
    return width * ratio;
}

// fit-content follows the picture's intrinsic width. A max-height cap makes
// the used picture narrower than that, and the frame would keep the extra
// space. The frame hugs the used width instead. A hair under an integer
// stays on that integer so a fractional layout pixel does not grow the frame.
export function bugReportFrameWidth(imageWidth: number): string {
    if (!(imageWidth > 0) || !Number.isFinite(imageWidth)) return "";
    return `${Math.ceil(imageWidth - 1e-4)}px`;
}

// Places the overlay on the image. left/top are relative to the shot box's
// padding edge (host rect is the border box, so the border is subtracted).
export function bugReportCanvasBox(
    image: { left: number; top: number; width: number; height: number },
    host: { left: number; top: number },
    borderLeft = 0,
    borderTop = 0,
): { left: number; top: number; width: number; height: number } | null {
    if (image.width <= 0 || image.height <= 0) return null;
    return {
        left: image.left - host.left - borderLeft,
        top: image.top - host.top - borderTop,
        width: image.width,
        height: image.height,
    };
}

export function bugReportExportSize(width: number, height: number): { width: number; height: number } | null {
    if (width <= 0 || height <= 0) return null;
    const edge = Math.max(width, height);
    if (edge <= BUG_REPORT_EXPORT_MAX_EDGE) {
        return { width: Math.round(width), height: Math.round(height) };
    }
    const scale = BUG_REPORT_EXPORT_MAX_EDGE / edge;
    return {
        width: Math.max(1, Math.round(width * scale)),
        height: Math.max(1, Math.round(height * scale)),
    };
}

export function paintBugReportMarks(
    ctx: BugReportDrawSurface,
    width: number,
    height: number,
    marks: readonly BugReportMark[],
): void {
    for (const mark of marks) paintBugReportMark(ctx, width, height, mark);
}

// Flattens the screenshot and its marks. An empty mark list returns "" so
// the caller can keep the original capture bytes. A canvas that cannot
// produce a PNG returns "" as well.
export function bugReportAnnotatedDataURL(
    image: CanvasImageSource,
    width: number,
    height: number,
    marks: readonly BugReportMark[],
): string {
    if (marks.length === 0) return "";
    const size = bugReportExportSize(width, height);
    if (!size || typeof document === "undefined") return "";
    const canvas = document.createElement("canvas");
    canvas.width = size.width;
    canvas.height = size.height;
    const ctx = canvas.getContext("2d");
    if (!ctx) return "";
    const scaled = size.width !== Math.round(width) || size.height !== Math.round(height);
    ctx.imageSmoothingEnabled = scaled;
    if (scaled) ctx.imageSmoothingQuality = "high";
    ctx.drawImage(image, 0, 0, size.width, size.height);
    paintBugReportMarks(ctx, size.width, size.height, marks);
    const url = canvas.toDataURL("image/png");
    return url.startsWith("data:image/png") ? url : "";
}

function paintBugReportMark(ctx: BugReportDrawSurface, width: number, height: number, mark: BugReportMark): void {
    if (mark.points.length === 0 || width <= 0 || height <= 0) return;
    const color = bugReportMarkColor(mark.color);
    const stroke = bugReportStrokeWidth(mark.tool, width);
    ctx.save();
    ctx.lineCap = "round";
    ctx.lineJoin = "round";
    ctx.strokeStyle = color;
    ctx.fillStyle = color;
    ctx.lineWidth = stroke;
    if (mark.tool === "highlight") ctx.globalAlpha = BUG_REPORT_HIGHLIGHT_ALPHA;
    if (mark.tool === "box") {
        if (mark.points.length >= 2) {
            const x1 = mark.points[0].x * width;
            const y1 = mark.points[0].y * height;
            const x2 = mark.points[1].x * width;
            const y2 = mark.points[1].y * height;
            ctx.strokeRect(Math.min(x1, x2), Math.min(y1, y2), Math.abs(x2 - x1), Math.abs(y2 - y1));
        }
        ctx.restore();
        return;
    }
    if (mark.points.length === 1) {
        const point = mark.points[0];
        ctx.beginPath();
        // Half the stroke, same ratio as the line. A pixel floor makes a
        // tap on a narrow preview larger than the same tap in the zip.
        ctx.arc(point.x * width, point.y * height, stroke / 2, 0, Math.PI * 2);
        ctx.fill();
        ctx.restore();
        return;
    }
    strokeBugReportPath(ctx, width, height, mark.points);
    ctx.restore();
}

function strokeBugReportPath(
    ctx: BugReportDrawSurface,
    width: number,
    height: number,
    points: readonly BugReportPoint[],
): void {
    const xOf = (point: BugReportPoint) => point.x * width;
    const yOf = (point: BugReportPoint) => point.y * height;
    ctx.beginPath();
    ctx.moveTo(xOf(points[0]), yOf(points[0]));
    // Vertices, not controls. A quadratic leaves the ink off the pointer
    // wherever a sample sits on the inside of a corner.
    for (let i = 1; i < points.length; i++) {
        ctx.lineTo(xOf(points[i]), yOf(points[i]));
    }
    ctx.stroke();
}

function clamp01(value: number): number {
    if (value < 0) return 0;
    if (value > 1) return 1;
    return value;
}
