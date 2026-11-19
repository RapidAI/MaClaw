import { describe, expect, it } from "vitest";
import {
    BUG_REPORT_EXPORT_MAX_EDGE,
    BUG_REPORT_HIGHLIGHT_ALPHA,
    BUG_REPORT_MARK_COLORS,
    BUG_REPORT_MARK_POINT_LIMIT,
    appendBugReportPoint,
    bugReportCanvasBox,
    bugReportExportSize,
    bugReportFrameWidth,
    bugReportImageReady,
    bugReportMarkColor,
    bugReportMarkWorthKeeping,
    bugReportPointerPoint,
    bugReportPointerSamples,
    bugReportStrokeWidth,
    paintBugReportMarks,
    type BugReportDrawSurface,
    type BugReportMark,
} from "../bugReportAnnotate";

class DrawRec implements BugReportDrawSurface {
    ops: string[] = [];
    strokeStyle: string | CanvasGradient | CanvasPattern = "";
    fillStyle: string | CanvasGradient | CanvasPattern = "";
    lineWidth = 0;
    lineCap: CanvasLineCap = "butt";
    lineJoin: CanvasLineJoin = "miter";
    globalAlpha = 1;
    private stack: Array<Pick<DrawRec, "strokeStyle" | "fillStyle" | "lineWidth" | "lineCap" | "lineJoin" | "globalAlpha">> = [];
    save() {
        this.ops.push("save");
        this.stack.push({
            strokeStyle: this.strokeStyle,
            fillStyle: this.fillStyle,
            lineWidth: this.lineWidth,
            lineCap: this.lineCap,
            lineJoin: this.lineJoin,
            globalAlpha: this.globalAlpha,
        });
    }
    restore() {
        this.ops.push("restore");
        const prev = this.stack.pop();
        if (!prev) return;
        this.strokeStyle = prev.strokeStyle;
        this.fillStyle = prev.fillStyle;
        this.lineWidth = prev.lineWidth;
        this.lineCap = prev.lineCap;
        this.lineJoin = prev.lineJoin;
        this.globalAlpha = prev.globalAlpha;
    }
    beginPath() { this.ops.push("begin"); }
    moveTo(x: number, y: number) { this.ops.push(`M ${x},${y}`); }
    lineTo(x: number, y: number) { this.ops.push(`L ${x},${y}`); }
    stroke() { this.ops.push(`stroke a=${this.globalAlpha} w=${this.lineWidth} ${this.strokeStyle}`); }
    fill() { this.ops.push(`fill a=${this.globalAlpha} ${this.fillStyle}`); }
    arc(_x: number, _y: number, radius: number) { this.ops.push(`arc r=${radius}`); }
    strokeRect(x: number, y: number, w: number, h: number) { this.ops.push(`rect ${x},${y},${w},${h}`); }
}

const rect = { left: 10, top: 20, width: 200, height: 100 };

describe("bug report marks", () => {
    it("clamps a pointer into the screenshot and ignores an empty box", () => {
        expect(bugReportPointerPoint(rect, 10, 20)).toEqual({ x: 0, y: 0 });
        expect(bugReportPointerPoint(rect, 110, 70)).toEqual({ x: 0.5, y: 0.5 });
        expect(bugReportPointerPoint(rect, -40, 400)).toEqual({ x: 0, y: 1 });
        expect(bugReportPointerPoint({ left: 0, top: 0, width: 0, height: 10 }, 1, 1)).toBeNull();
    });

    it("keeps coalesced pointer samples and falls back to the event", () => {
        expect(bugReportPointerSamples({ clientX: 1, clientY: 2 })).toEqual([{ clientX: 1, clientY: 2 }]);
        expect(bugReportPointerSamples({
            clientX: 1,
            clientY: 2,
            getCoalescedEvents: () => [],
        })).toEqual([{ clientX: 1, clientY: 2 }]);
        expect(bugReportPointerSamples({
            clientX: 9,
            clientY: 9,
            getCoalescedEvents: () => [{ clientX: 3, clientY: 4 }, { clientX: 5, clientY: 6 }],
        })).toEqual([{ clientX: 3, clientY: 4 }, { clientX: 5, clientY: 6 }]);
    });

    it("keeps a box origin and drops a pen point that did not move", () => {
        const box: BugReportMark = { tool: "box", color: "#E5484D", points: [{ x: 0.1, y: 0.2 }] };
        const moved = appendBugReportPoint(box, { x: 0.4, y: 0.1 });
        expect(moved.points).toEqual([{ x: 0.1, y: 0.2 }, { x: 0.4, y: 0.1 }]);
        expect(bugReportMarkWorthKeeping(moved)).toBe(true);
        expect(bugReportMarkWorthKeeping({ ...box, points: [{ x: 0.1, y: 0.2 }, { x: 0.101, y: 0.201 }] })).toBe(false);

        const pen: BugReportMark = { tool: "pen", color: "#1C2430", points: [{ x: 0.2, y: 0.2 }] };
        expect(appendBugReportPoint(pen, { x: 0.2004, y: 0.2004 }).points).toHaveLength(1);
        expect(appendBugReportPoint(pen, { x: 0.3, y: 0.4 }).points).toHaveLength(2);
        expect(pen.points).toHaveLength(1);
        expect(bugReportMarkWorthKeeping({ tool: "highlight", color: "#F5C518", points: [] })).toBe(false);
    });

    it("stops a freehand stroke at the point limit", () => {
        let mark: BugReportMark = { tool: "pen", color: "#1C2430", points: [{ x: 0, y: 0 }] };
        for (let i = 0; i < BUG_REPORT_MARK_POINT_LIMIT + 10; i++) {
            mark = appendBugReportPoint(mark, { x: i % 2 === 0 ? 0.8 : 0.2, y: i % 2 === 0 ? 0.2 : 0.8 });
        }
        expect(mark.points.length).toBe(BUG_REPORT_MARK_POINT_LIMIT);
    });

    it("paints a highlighter wash, then an opaque pen, a dot, and a box", () => {
        const rec = new DrawRec();
        const width = 200;
        const height = 100;
        paintBugReportMarks(rec, width, height, [
            { tool: "highlight", color: "#F5C518", points: [{ x: 0.1, y: 0.2 }, { x: 0.5, y: 0.6 }] },
            { tool: "pen", color: "#E5484D", points: [{ x: 0.2, y: 0.2 }, { x: 0.4, y: 0.8 }] },
            { tool: "pen", color: "#3B82F6", points: [{ x: 0.5, y: 0.5 }] },
            { tool: "box", color: "not-a-color", points: [{ x: 0.1, y: 0.2 }, { x: 0.6, y: 0.7 }] },
        ]);
        expect(rec.ops[0]).toBe("save");
        expect(rec.ops).toContain(`stroke a=${BUG_REPORT_HIGHLIGHT_ALPHA} w=${bugReportStrokeWidth("highlight", width)} #F5C518`);
        expect(rec.ops).toContain(`M ${0.1 * width},${0.2 * height}`);
        expect(rec.ops).toContain(`stroke a=1 w=${bugReportStrokeWidth("pen", width)} #E5484D`);
        expect(rec.ops).toContain(`arc r=${bugReportStrokeWidth("pen", width) / 2}`);
        expect(rec.ops).toContain("fill a=1 #3B82F6");
        expect(rec.ops).toContain(`rect ${0.1 * width},${0.2 * height},${0.5 * width},${0.5 * height}`);
        expect(rec.ops.filter((op) => op === "restore")).toHaveLength(4);
        expect(rec.globalAlpha).toBe(1);
        expect(bugReportMarkColor("nope")).toBe(BUG_REPORT_MARK_COLORS[0].value);
    });

    it("scales stroke width with the picture", () => {
        const narrow = bugReportStrokeWidth("highlight", 400);
        const wide = bugReportStrokeWidth("highlight", 2000);
        expect(wide / narrow).toBeCloseTo(5, 5);
        expect(bugReportStrokeWidth("pen", 2000) / bugReportStrokeWidth("pen", 400)).toBeCloseTo(5, 5);
        expect(bugReportStrokeWidth("box", 100)).toBeCloseTo(0.3, 5);
    });

    it("pins the overlay to the image inside the shot border", () => {
        expect(bugReportCanvasBox(
            { left: 111, top: 81, width: 746, height: 420 },
            { left: 100, top: 60 },
            1,
            1,
        )).toEqual({ left: 10, top: 20, width: 746, height: 420 });
        expect(bugReportCanvasBox({ left: 0, top: 0, width: 0, height: 10 }, { left: 0, top: 0 })).toBeNull();
        expect(bugReportFrameWidth(0)).toBe("");
        expect(bugReportFrameWidth(Number.NaN)).toBe("");
        expect(bugReportFrameWidth(200)).toBe("200px");
        expect(bugReportFrameWidth(746.2)).toBe("747px");
    });

    it("draws a freehand stroke through every sample", () => {
        const rec = new DrawRec();
        paintBugReportMarks(rec, 100, 100, [
            { tool: "pen", color: "#E5484D", points: [{ x: 0, y: 0 }, { x: 0.5, y: 0 }, { x: 1, y: 1 }] },
        ]);
        expect(rec.ops).toContain("M 0,0");
        expect(rec.ops).toContain("L 50,0");
        expect(rec.ops).toContain("L 100,100");

        const longer = new DrawRec();
        paintBugReportMarks(longer, 100, 100, [
            {
                tool: "pen",
                color: "#E5484D",
                points: [{ x: 0, y: 0 }, { x: 0, y: 1 }, { x: 1, y: 1 }, { x: 1, y: 0 }],
            },
        ]);
        expect(longer.ops).toContain("L 0,100");
        expect(longer.ops).toContain("L 100,100");
        expect(longer.ops).toContain("L 100,0");
    });

    it("treats a finished bitmap as ready and an empty one as not", () => {
        expect(bugReportImageReady(null)).toBe(false);
        expect(bugReportImageReady({ complete: true, naturalWidth: 0 })).toBe(false);
        expect(bugReportImageReady({ complete: false, naturalWidth: 20 })).toBe(false);
        expect(bugReportImageReady({ complete: true, naturalWidth: 20 })).toBe(true);
    });

    it("keeps a normal window at its own size and caps a huge one", () => {
        expect(bugReportExportSize(1920, 1080)).toEqual({ width: 1920, height: 1080 });
        expect(bugReportExportSize(8000, 2000)).toEqual({ width: BUG_REPORT_EXPORT_MAX_EDGE, height: 1024 });
        expect(bugReportExportSize(0, 10)).toBeNull();
    });
});
