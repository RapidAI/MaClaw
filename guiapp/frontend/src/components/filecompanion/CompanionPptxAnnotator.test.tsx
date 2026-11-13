// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { hydratePdfMarks, loadPdfMarks } from "./pdfMarks";
import { hydratePdfDrawings, loadPdfDrawings } from "./pdfDrawings";
import { hydratePdfBookmarks, loadPdfBookmarks } from "./pdfBookmarks";
import { hydratePdfTranslations, loadPdfTranslations } from "./pdfTranslations";
import { CompanionPptxAnnotator } from "./CompanionPptxAnnotator";

const PATH = "/tmp/deck.pptx";

function renderSlide(page = 2, onJumpSlide = vi.fn()) {
    return render(
        <CompanionPptxAnnotator path={PATH} page={page} lang="zh" onJumpSlide={onJumpSlide}>
            <img alt="slide" />
        </CompanionPptxAnnotator>,
    );
}

function stageRect() {
    const stage = screen.getByTestId("pptx-annotator-stage");
    vi.spyOn(stage, "getBoundingClientRect").mockReturnValue({
        left: 0, top: 0, right: 200, bottom: 100, width: 200, height: 100, x: 0, y: 0, toJSON: () => ({}),
    } as DOMRect);
    return stage;
}

function openMenu(x = 30, y = 30) {
    const stage = stageRect();
    fireEvent.contextMenu(stage, { clientX: x, clientY: y });
    return stage;
}

beforeEach(() => {
    hydratePdfMarks({});
    hydratePdfDrawings({});
    hydratePdfBookmarks({});
    hydratePdfTranslations({});
    delete (window as unknown as { go?: unknown }).go;
});

describe("CompanionPptxAnnotator", () => {
    it("wraps the slide image in an annotator stage", () => {
        renderSlide();
        const stage = screen.getByTestId("pptx-annotator-stage");
        expect(stage.querySelector("img")).toBeTruthy();
    });

    it("creates a rect highlight on the current slide through the menu", async () => {
        renderSlide(2);
        openMenu(30, 30);
        fireEvent.mouseDown(await screen.findByTestId("pptx-rect-yellow"));
        const stage = screen.getByTestId("pptx-annotator-stage");
        fireEvent.mouseDown(stage, { clientX: 20, clientY: 20, button: 0 });
        fireEvent.mouseMove(window, { clientX: 100, clientY: 60 });
        fireEvent.mouseUp(window, { clientX: 100, clientY: 60 });
        await waitFor(() => {
            const marks = loadPdfMarks(PATH);
            expect(marks).toHaveLength(1);
            expect(marks[0].page).toBe(2);
            expect(marks[0].rects[0]).toMatchObject({ x: 0.1, y: 0.2 });
        });
        const mark = loadPdfMarks(PATH)[0];
        expect(screen.getByTestId(`pptx-mark-${mark.id}`)).toBeTruthy();
    });

    it("clears a mark under the pointer from its menu", async () => {
        renderSlide(2);
        openMenu(30, 30);
        fireEvent.mouseDown(await screen.findByTestId("pptx-rect-green"));
        const stage = screen.getByTestId("pptx-annotator-stage");
        fireEvent.mouseDown(stage, { clientX: 10, clientY: 10, button: 0 });
        fireEvent.mouseUp(window, { clientX: 60, clientY: 40 });
        await waitFor(() => expect(loadPdfMarks(PATH)).toHaveLength(1));
        // Right-click inside the painted rect (0.05,0.10)-(0.30,0.40) → point (20,20).
        fireEvent.contextMenu(stage, { clientX: 20, clientY: 20 });
        fireEvent.mouseDown(await screen.findByTestId("pptx-clear-mark"));
        await waitFor(() => expect(loadPdfMarks(PATH)).toHaveLength(0));
    });

    it("writes a note anchored to the click point and opens its card", async () => {
        renderSlide(1);
        openMenu(50, 40);
        fireEvent.mouseDown(await screen.findByTestId("pptx-menu-annotate"));
        fireEvent.change(await screen.findByTestId("pptx-annotation-input"), { target: { value: "这一页要核对" } });
        fireEvent.click(screen.getByTestId("pptx-annotation-save"));
        await waitFor(() => {
            const notes = loadPdfTranslations(PATH).filter((note) => note.kind === "annotation");
            expect(notes).toHaveLength(1);
            expect(notes[0].translation).toBe("这一页要核对");
            expect(notes[0].parts[0].page).toBe(1);
        });
        const note = loadPdfTranslations(PATH)[0];
        expect(screen.getByTestId(`pptx-note-pin-${note.id}`)).toBeTruthy();
        expect(screen.getByTestId(`pptx-note-card-${note.id}`)).toBeTruthy();
    });

    it("adds a slide bookmark and shows it in the rail", async () => {
        renderSlide(3);
        expect(screen.queryByTestId("pptx-annotator-rail")).toBeNull();
        openMenu(60, 60);
        fireEvent.mouseDown(await screen.findByTestId("pptx-bookmark-add"));
        fireEvent.change(await screen.findByTestId("pptx-bookmark-name"), { target: { value: "目录页" } });
        fireEvent.click(screen.getByTestId("pptx-bookmark-save"));
        await waitFor(() => {
            const bookmarks = loadPdfBookmarks(PATH);
            expect(bookmarks).toHaveLength(1);
            expect(bookmarks[0]).toMatchObject({ name: "目录页", page: 3 });
        });
        const bookmark = loadPdfBookmarks(PATH)[0];
        expect(screen.getByTestId(`pptx-bookmark-tick-${bookmark.id}`)).toBeTruthy();
        expect(screen.getByTestId(`annotator-rail-bookmark-${bookmark.id}`)).toBeTruthy();
    });

    it("jumps to a bookmarked slide from the rail", async () => {
        const onJumpSlide = vi.fn();
        renderSlide(1, onJumpSlide);
        openMenu(10, 10);
        fireEvent.mouseDown(await screen.findByTestId("pptx-bookmark-add"));
        fireEvent.change(await screen.findByTestId("pptx-bookmark-name"), { target: { value: "第二页" } });
        fireEvent.click(screen.getByTestId("pptx-bookmark-save"));
        const bookmark = loadPdfBookmarks(PATH)[0];
        fireEvent.click(await screen.findByTestId(`annotator-rail-bookmark-${bookmark.id}`));
        expect(onJumpSlide).toHaveBeenCalledWith(1);
    });

    it("draws a line on the slide through the pen panel", async () => {
        renderSlide(1);
        fireEvent.click(screen.getByTestId("pptx-draw-toggle"));
        const panel = await screen.findByTestId("pptx-draw-panel");
        fireEvent.click(panel.querySelector('[data-testid="pptx-draw-line"]')!);
        const layer = screen.getByTestId("pptx-drawing-layer");
        vi.spyOn(layer, "getBoundingClientRect").mockReturnValue({
            left: 0, top: 0, right: 200, bottom: 100, width: 200, height: 100, x: 0, y: 0, toJSON: () => ({}),
        } as DOMRect);
        fireEvent.mouseDown(layer, { clientX: 20, clientY: 20, button: 0 });
        fireEvent.mouseMove(window, { clientX: 120, clientY: 60 });
        fireEvent.mouseUp(window, { clientX: 120, clientY: 60 });
        await waitFor(() => {
            const drawings = loadPdfDrawings(PATH);
            expect(drawings).toHaveLength(1);
            expect(drawings[0]).toMatchObject({ page: 1, tool: "line" });
            expect(drawings[0].points[0]).toMatchObject({ x: 0.1, y: 0.2 });
        });
    });

    it("keeps slide marks apart by page", async () => {
        renderSlide(1);
        openMenu(30, 30);
        fireEvent.mouseDown(await screen.findByTestId("pptx-rect-yellow"));
        const stage = screen.getByTestId("pptx-annotator-stage");
        fireEvent.mouseDown(stage, { clientX: 10, clientY: 10, button: 0 });
        fireEvent.mouseUp(window, { clientX: 50, clientY: 40 });
        await waitFor(() => expect(loadPdfMarks(PATH)).toHaveLength(1));
        // Same path on slide 2: the slide-1 mark stays stored but is not painted.
        renderSlide(2);
        await waitFor(() => expect(loadPdfMarks(PATH)).toHaveLength(1));
        const mark = loadPdfMarks(PATH)[0];
        expect(mark.page).toBe(1);
        const marks = screen.queryAllByTestId(`pptx-mark-${mark.id}`);
        expect(marks).toHaveLength(1);
    });
});
