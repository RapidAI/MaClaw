// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { hydrateChatAnnotations, loadChatAnnotations, saveChatAnnotations } from "./chatAnnotations";
import { DOC_MARK_MESSAGE, hydrateChatMarks, loadChatMarks, saveChatMarks } from "./chatMarks";
import { hydratePdfBookmarks, loadPdfBookmarks } from "./pdfBookmarks";
import { hydratePdfDrawings } from "./pdfDrawings";
import { loadDocBookmarks, loadDocDrawings, newDocBookmark, saveDocAnnotations, saveDocBookmarks, saveDocMarks, loadDocMarks } from "./docMarks";
import { CompanionDocAnnotator } from "./CompanionDocAnnotator";

let registry: Map<string, Set<Range>>;

beforeEach(() => {
    registry = new Map();
    (globalThis as unknown as { Highlight: unknown }).Highlight = class Highlight {
        readonly ranges: Set<Range>;
        constructor(...ranges: Range[]) {
            this.ranges = new Set(ranges);
        }
        get size() {
            return this.ranges.size;
        }
    };
    (globalThis as unknown as { CSS: unknown }).CSS = {
        highlights: {
            set: (name: string, value: Set<Range>) => registry.set(name, value),
            delete: (name: string) => registry.delete(name),
            get: (name: string) => registry.get(name),
        },
    };
    hydrateChatMarks({});
    hydrateChatAnnotations({});
    hydratePdfBookmarks({});
    hydratePdfDrawings({});
    delete (window as unknown as { go?: unknown }).go;
});

afterEach(() => {
    delete (globalThis as unknown as { Highlight?: unknown }).Highlight;
    const css = (globalThis as unknown as { CSS?: { highlights?: unknown } }).CSS;
    if (css) delete css.highlights;
    window.getSelection()?.removeAllRanges();
});

function renderDoc(text = "Hello world document body") {
    return render(
        <CompanionDocAnnotator path="/tmp/a.md" lang="zh">
            <p>{text}</p>
        </CompanionDocAnnotator>,
    );
}

function selectIn(host: HTMLElement, start: number, end: number): Range {
    const text = host.querySelector("p")!.firstChild!;
    const range = document.createRange();
    range.setStart(text, start);
    range.setEnd(text, end);
    const selection = window.getSelection();
    selection?.removeAllRanges();
    selection?.addRange(range);
    return range;
}

describe("CompanionDocAnnotator", () => {
    it("renders the document inside a document-message host", () => {
        renderDoc();
        const host = screen.getByTestId("doc-annotator-page");
        expect(host.getAttribute("data-chat-message")).toBe(String(DOC_MARK_MESSAGE));
        expect(host.textContent).toContain("Hello world");
    });

    it("paints a right-click selection into the document marks store", async () => {
        renderDoc();
        const host = screen.getByTestId("doc-annotator-page");
        selectIn(host, 0, 5);
        fireEvent.contextMenu(host.querySelector("p")!.firstChild!, { clientX: 10, clientY: 10 });
        const menu = await screen.findByTestId("chat-highlight-menu");
        fireEvent.mouseDown(menu.querySelector('[data-testid="chat-highlight-yellow"]')!);
        await waitFor(() => {
            const marks = loadDocMarks("/tmp/a.md");
            expect(marks).toHaveLength(1);
            expect(marks[0]).toMatchObject({ message: DOC_MARK_MESSAGE, color: "yellow", start: 0, end: 5, text: "Hello" });
        });
        expect(registry.get("companion-doc-yellow")?.size).toBe(1);
    });

    it("announces a local paint through the changed event", async () => {
        const listener = vi.fn();
        window.addEventListener("maclaw:companion-chat-marks-changed", listener);
        renderDoc();
        const host = screen.getByTestId("doc-annotator-page");
        selectIn(host, 0, 5);
        fireEvent.contextMenu(host.querySelector("p")!.firstChild!, { clientX: 10, clientY: 10 });
        const menu = await screen.findByTestId("chat-highlight-menu");
        fireEvent.mouseDown(menu.querySelector('[data-testid="chat-highlight-green"]')!);
        await waitFor(() => expect(listener).toHaveBeenCalled());
        window.removeEventListener("maclaw:companion-chat-marks-changed", listener);
    });

    it("repaints when a share writes marks without announcing", async () => {
        renderDoc();
        expect(loadDocMarks("/tmp/a.md")).toHaveLength(0);
        // A received share applies to the same store with announce = false.
        saveChatMarks("/tmp/a.md", [
            { message: DOC_MARK_MESSAGE, color: "blue", start: 0, end: 5, text: "Hello" },
            { message: 3, color: "pink", start: 0, end: 4, text: "chat" },
        ]);
        await waitFor(() => {
            expect(loadDocMarks("/tmp/a.md")).toHaveLength(1);
            expect(registry.get("companion-doc-blue")?.size).toBe(1);
        });
    });

    it("writes a note on the selection through the annotation editor", async () => {
        renderDoc();
        const host = screen.getByTestId("doc-annotator-page");
        selectIn(host, 0, 5);
        fireEvent.contextMenu(host.querySelector("p")!.firstChild!, { clientX: 10, clientY: 10 });
        const menu = await screen.findByTestId("chat-highlight-menu");
        fireEvent.mouseDown(menu.querySelector('[data-testid="chat-menu-annotate"]')!);
        const input = await screen.findByTestId("chat-annotation-input");
        fireEvent.change(input, { target: { value: "这一段说得对" } });
        fireEvent.click(screen.getByTestId("chat-annotation-save"));
        await waitFor(() => {
            const notes = loadChatAnnotations("/tmp/a.md");
            expect(notes).toHaveLength(1);
            expect(notes[0]).toMatchObject({ message: DOC_MARK_MESSAGE, body: "这一段说得对", text: "Hello" });
        });
    });

    it("adds a bookmark from the selection and removes it from the ribbon", async () => {
        renderDoc();
        const host = screen.getByTestId("doc-annotator-page");
        selectIn(host, 0, 5);
        fireEvent.contextMenu(host.querySelector("p")!.firstChild!, { clientX: 10, clientY: 10 });
        const menu = await screen.findByTestId("chat-highlight-menu");
        fireEvent.mouseDown(menu.querySelector('[data-testid="doc-bookmark-add"]')!);
        const name = await screen.findByTestId("doc-bookmark-name");
        expect((name as HTMLInputElement).value).toBe("Hello");
        fireEvent.click(screen.getByTestId("doc-bookmark-save"));
        await waitFor(() => expect(loadPdfBookmarks("/tmp/a.md")).toHaveLength(1));
        const bookmark = loadPdfBookmarks("/tmp/a.md")[0];
        expect(bookmark).toMatchObject({ page: 1, name: "Hello" });
        // The ribbon hangs on the document; right-clicking it offers the
        // bookmark actions instead of the text menu.
        const tick = await screen.findByTestId(`doc-bookmark-tick-${bookmark.id}`);
        fireEvent.contextMenu(tick, { clientX: 5, clientY: 5 });
        const ribbonMenu = await screen.findByTestId("chat-highlight-menu");
        fireEvent.mouseDown(ribbonMenu.querySelector('[data-testid="doc-bookmark-delete"]')!);
        await waitFor(() => expect(loadPdfBookmarks("/tmp/a.md")).toHaveLength(0));
    });

    it("clears the highlight under the caret on a bare right-click", async () => {
        saveDocMarks("/tmp/a.md", [{ message: DOC_MARK_MESSAGE, color: "yellow", start: 0, end: 5, text: "Hello" }]);
        const doc = document as Document & { caretRangeFromPoint?: (x: number, y: number) => Range | null };
        const hostRefForCaret = { host: null as HTMLElement | null };
        doc.caretRangeFromPoint = () => {
            const text = hostRefForCaret.host?.querySelector("p")?.firstChild;
            if (!text) return null;
            const range = document.createRange();
            range.setStart(text, 2);
            range.setEnd(text, 2);
            return range;
        };
        try {
            renderDoc();
            const host = screen.getByTestId("doc-annotator-page");
            hostRefForCaret.host = host;
            fireEvent.contextMenu(host.querySelector("p")!.firstChild!, { clientX: 10, clientY: 10 });
            const menu = await screen.findByTestId("chat-highlight-menu");
            fireEvent.mouseDown(menu.querySelector('[data-testid="chat-highlight-clear"]')!);
            await waitFor(() => expect(loadDocMarks("/tmp/a.md")).toHaveLength(0));
        } finally {
            delete (doc as { caretRangeFromPoint?: unknown }).caretRangeFromPoint;
        }
    });

    it("renames a bookmark from its ribbon", async () => {
        saveDocMarks("/tmp/a.md", []);
        renderDoc();
        const host = screen.getByTestId("doc-annotator-page");
        selectIn(host, 0, 5);
        fireEvent.contextMenu(host.querySelector("p")!.firstChild!, { clientX: 10, clientY: 10 });
        const menu = await screen.findByTestId("chat-highlight-menu");
        fireEvent.mouseDown(menu.querySelector('[data-testid="doc-bookmark-add"]')!);
        await screen.findByTestId("doc-bookmark-name");
        fireEvent.click(screen.getByTestId("doc-bookmark-save"));
        await waitFor(() => expect(loadPdfBookmarks("/tmp/a.md")).toHaveLength(1));
        const bookmark = loadPdfBookmarks("/tmp/a.md")[0];
        const tick = await screen.findByTestId(`doc-bookmark-tick-${bookmark.id}`);
        fireEvent.contextMenu(tick, { clientX: 5, clientY: 5 });
        const ribbonMenu = await screen.findByTestId("chat-highlight-menu");
        fireEvent.mouseDown(ribbonMenu.querySelector('[data-testid="doc-bookmark-rename"]')!);
        const name = await screen.findByTestId("doc-bookmark-name");
        fireEvent.change(name, { target: { value: "第二章" } });
        fireEvent.click(screen.getByTestId("doc-bookmark-color-teal"));
        fireEvent.click(screen.getByTestId("doc-bookmark-save"));
        await waitFor(() => {
            const saved = loadPdfBookmarks("/tmp/a.md")[0];
            expect(saved).toMatchObject({ id: bookmark.id, name: "第二章", color: "teal" });
        });
    });

    it("keeps document and transcript marks apart on one path", async () => {
        saveChatMarks("/tmp/a.md", [{ message: 0, color: "pink", start: 0, end: 4, text: "chat" }]);
        renderDoc();
        const host = screen.getByTestId("doc-annotator-page");
        selectIn(host, 0, 5);
        fireEvent.contextMenu(host.querySelector("p")!.firstChild!, { clientX: 10, clientY: 10 });
        const menu = await screen.findByTestId("chat-highlight-menu");
        fireEvent.mouseDown(menu.querySelector('[data-testid="chat-highlight-blue"]')!);
        await waitFor(() => expect(loadDocMarks("/tmp/a.md")).toHaveLength(1));
        const all = loadChatMarks("/tmp/a.md");
        expect(all).toHaveLength(2);
        expect(all.some((mark) => mark.message === 0)).toBe(true);
        expect(all.some((mark) => mark.message === DOC_MARK_MESSAGE)).toBe(true);
    });
});

describe("CompanionDocAnnotator styles and rail", () => {
    it("paints an underline mark through the 画线 menu row", async () => {
        renderDoc();
        const host = screen.getByTestId("doc-annotator-page");
        selectIn(host, 0, 5);
        fireEvent.contextMenu(host.querySelector("p")!.firstChild!, { clientX: 10, clientY: 10 });
        const menu = await screen.findByTestId("chat-highlight-menu");
        fireEvent.mouseDown(menu.querySelector('[data-testid="chat-line-underline-yellow"]')!);
        await waitFor(() => {
            const marks = loadDocMarks("/tmp/a.md");
            expect(marks).toHaveLength(1);
            expect(marks[0]).toMatchObject({ color: "yellow", start: 0, end: 5, style: "underline" });
        });
        expect(registry.get("companion-doc-underline-yellow")?.size).toBe(1);
    });

    it("paints a strike mark into its own highlight bucket", async () => {
        renderDoc();
        const host = screen.getByTestId("doc-annotator-page");
        selectIn(host, 6, 11);
        fireEvent.contextMenu(host.querySelector("p")!.firstChild!, { clientX: 10, clientY: 10 });
        const menu = await screen.findByTestId("chat-highlight-menu");
        fireEvent.mouseDown(menu.querySelector('[data-testid="chat-line-strike-blue"]')!);
        await waitFor(() => {
            const marks = loadDocMarks("/tmp/a.md");
            expect(marks[0]).toMatchObject({ color: "blue", style: "strike" });
        });
        expect(registry.get("companion-doc-strike-blue")?.size).toBe(1);
    });

    it("shows the PDF-like rail once a bookmark exists", async () => {
        renderDoc();
        expect(screen.queryByTestId("doc-annotator-rail")).toBeNull();
        saveDocBookmarks("/tmp/a.md", [newDocBookmark("第一章", "yellow", 0.2)]);
        await waitFor(() => expect(screen.getByTestId("doc-annotator-rail")).toBeTruthy());
        const bookmark = loadDocBookmarks("/tmp/a.md")[0];
        expect(screen.getByTestId(`annotator-rail-bookmark-${bookmark.id}`)).toBeTruthy();
    });

    it("lists a document note in the rail", async () => {
        saveDocAnnotations("/tmp/a.md", [{ id: "n1", message: DOC_MARK_MESSAGE, start: 0, end: 5, text: "Hello", body: "正文", name: "第一处批注" }]);
        renderDoc();
        await waitFor(() => expect(screen.getByTestId("doc-annotator-rail")).toBeTruthy());
        expect(screen.getByTestId("annotator-rail-note-n1").textContent).toContain("第一处批注");
    });

    it("opens the draw panel from the toggle and records a line", async () => {
        renderDoc();
        fireEvent.click(screen.getByTestId("doc-draw-toggle"));
        const panel = await screen.findByTestId("doc-draw-panel");
        fireEvent.click(panel.querySelector('[data-testid="doc-draw-line"]')!);
        const layer = screen.getByTestId("doc-drawing-layer");
        const rectSpy = vi.spyOn(layer, "getBoundingClientRect").mockReturnValue({
            left: 0, top: 0, right: 200, bottom: 200, width: 200, height: 200, x: 0, y: 0, toJSON: () => ({}),
        } as DOMRect);
        fireEvent.mouseDown(layer, { clientX: 20, clientY: 20, button: 0 });
        fireEvent.mouseMove(window, { clientX: 120, clientY: 120 });
        fireEvent.mouseUp(window, { clientX: 120, clientY: 120 });
        await waitFor(() => {
            const drawings = loadDocDrawings("/tmp/a.md");
            expect(drawings).toHaveLength(1);
            expect(drawings[0].tool).toBe("line");
            expect(drawings[0].points[0]).toMatchObject({ x: 0.1, y: 0.1 });
            expect(drawings[0].points[drawings[0].points.length - 1]).toMatchObject({ x: 0.6, y: 0.6 });
        });
        rectSpy.mockRestore();
    });
});
