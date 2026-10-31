// @vitest-environment jsdom
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { act, cleanup, createEvent, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { StrictMode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { clampPdfBookmarkRailWidth, CompanionPdfReader, nextPdfBookmarkRailPreferred, PDF_BOOKMARK_RAIL_STORAGE_KEY } from "../CompanionPdfReader";
import { hydratePdfMarks, loadPdfMarks, savePdfMarks } from "../../filecompanion/pdfMarks";
import { hydratePdfDrawings, loadPdfDrawings, PDF_DRAWINGS_CHANGED_EVENT } from "../../filecompanion/pdfDrawings";
import { hydratePdfBookmarks, loadPdfBookmarks, PDF_BOOKMARKS_CHANGED_EVENT } from "../../filecompanion/pdfBookmarks";
import { hydratePdfTranslations, loadPdfTranslations, PDF_ANNOTATIONS_CHANGED_EVENT, resetPdfTranslationFlight, savePdfTranslations } from "../../filecompanion/pdfTranslations";
import { hydrateCompanionReaderState } from "../../filecompanion/companionReaderState";
import type { CodePreviewTheme } from "../FileTabBar";

const getDocument = vi.fn();
const preview = vi.fn();
const translate = vi.fn();

vi.mock("pdfjs-dist", () => ({
    GlobalWorkerOptions: { workerSrc: "" },
    getDocument: (...args: unknown[]) => getDocument(...args),
}));

vi.mock("../../../../wailsjs/go/main/App", () => ({
    PreviewTaskResultFile: (...args: unknown[]) => preview(...args),
    TranslateFileCompanionSelection: (...args: unknown[]) => translate(...args),
}));

const theme = { bg: "#fff", text: "#111" } as CodePreviewTheme;
const previewURL = "/maclaw-preview/v1/file?t=0123456789abcdef0123456789abcdef";

function pdfWithPages(count: number, text = "Hello world", size = { width: 200, height: 400 }) {
    const getPage = vi.fn(async (page: number) => ({
        getViewport: ({ scale }: { scale: number }) => ({
            width: size.width * scale,
            height: size.height * scale,
            scale,
            transform: [scale, 0, 0, -scale, 0, size.height * scale],
        }),
        getTextContent: async () => ({
            items: page === 1 ? [{ str: text, transform: [12, 0, 0, 12, 40, 300], width: 80, height: 12 }] : [],
        }),
        render: () => ({ promise: Promise.resolve(), cancel: () => undefined }),
    }));
    getDocument.mockImplementation((params: { url?: string; data?: unknown }) => ({
        promise: Promise.resolve({ numPages: count, getPage, destroy: () => undefined }),
        destroy: () => undefined,
        params,
    }));
    return getPage;
}

function box(node: Element, width: number, height: number) {
    node.getBoundingClientRect = () => ({
        x: 0, y: 0, left: 0, top: 0, right: width, bottom: height, width, height, toJSON() { return {}; },
    });
}

function clientRect(left: number, top: number, width: number, height: number) {
    return {
        x: left, y: top, left, top,
        right: left + width, bottom: top + height,
        width, height,
        toJSON() { return {}; },
    };
}

function selectHello(page: HTMLElement) {
    box(page, 200, 400);
    const rect = { left: 10, top: 20, right: 50, bottom: 32, width: 40, height: 12 };
    vi.spyOn(window, "getSelection").mockReturnValue({
        isCollapsed: false,
        rangeCount: 1,
        toString: () => "Hello",
        removeAllRanges: () => undefined,
        getRangeAt: () => ({ getClientRects: () => [rect] }),
    } as unknown as Selection);
}

beforeEach(() => {
    cleanup();
    resetPdfTranslationFlight();
    hydratePdfMarks({});
    hydratePdfTranslations({});
    hydratePdfDrawings({});
    hydratePdfBookmarks({});
    delete (window as unknown as { go?: unknown }).go;
    getDocument.mockReset();
    preview.mockReset();
    translate.mockReset();
    preview.mockResolvedValue({ preview_url: previewURL });
    translate.mockResolvedValue("你好");
    vi.restoreAllMocks();
    class QuietObserver {
        observe() {}
        unobserve() {}
        disconnect() {}
    }
    vi.stubGlobal("IntersectionObserver", QuietObserver);
    localStorage.removeItem(PDF_BOOKMARK_RAIL_STORAGE_KEY);
});

describe("companion pdf reader", () => {
    it("opens a highlight menu on the selection and keeps the colour across a remount", async () => {
        pdfWithPages(1);
        const view = render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        expect(await screen.findByText("Hello world")).toBeTruthy();
        await waitFor(() => expect(getDocument).toHaveBeenCalled());
        const params = getDocument.mock.calls[0][0] as { url?: string; data?: unknown };
        expect(params.url).toBe(previewURL);
        expect(params.data).toBeUndefined();

        const page = screen.getByTestId("companion-pdf-page-1");
        selectHello(page);
        fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });
        const highlightMenu = screen.getByTestId("pdf-highlight-menu");
        expect(highlightMenu.textContent).toContain("高亮");
        expect(highlightMenu.querySelector(".companion-pdf-menu-title svg rect")?.getAttribute("fill")).toBe("#ffe08a");
        expect(screen.getByTestId("pdf-translate").querySelector("svg path[d='m22 22-5-10-5 10']")).toBeTruthy();
        expect(readFileSync(resolve("src/styles/partials/110-mc-app-shell.css"), "utf8")).toMatch(/\.companion-pdf-menu-color span\s*\{[^}]*box-sizing:\s*border-box/);
        for (const color of ["yellow", "green", "blue", "pink"]) {
            expect(screen.getByTestId(`pdf-highlight-${color}`)).toBeTruthy();
        }
        fireEvent.mouseDown(screen.getByTestId("pdf-highlight-green"));
        const mark = document.querySelector("[data-color='green']");
        expect(mark).toBeTruthy();
        expect((mark as HTMLElement).style.left).toBe("5%");
        expect(loadPdfMarks("C:/docs/Paper.PDF").some((item) => item.color === "green")).toBe(true);

        view.unmount();
        const again = render(<CompanionPdfReader absPath="c:/docs/paper.pdf" theme={theme} lang="zh-Hans" />);
        expect(await screen.findByText("Hello world")).toBeTruthy();
        await waitFor(() => expect(document.querySelector("[data-color='green']")).toBeTruthy());
        again.unmount();

        render(<CompanionPdfReader absPath="C:/docs/other.pdf" theme={theme} lang="zh-Hans" />);
        await screen.findByText("Hello world");
        expect(document.querySelector("[data-color='green']")).toBeNull();
    });

    it("paints highlights that arrive after the reader mounted", async () => {
        pdfWithPages(1);
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        expect(await screen.findByText("Hello world")).toBeTruthy();
        expect(document.querySelector("[data-color='green']")).toBeNull();

        (window as unknown as { go: unknown }).go = {
            main: {
                App: {
                    LoadCompanionReaderState: async () => JSON.stringify({
                        pdf_marks: {
                            "c:/docs/paper.pdf": [{
                                id: "late",
                                page: 1,
                                color: "green",
                                rects: [{ x: 0.05, y: 0.05, w: 0.2, h: 0.03 }],
                                text: "Hello",
                            }],
                        },
                        pdf_translations: {
                            "c:/docs/paper.pdf": [{
                                id: "late-t",
                                text: "Hello",
                                translation: "你好",
                                parts: [{ page: 1, rects: [{ x: 0.05, y: 0.05, w: 0.2, h: 0.03 }] }],
                            }],
                        },
                        input_history: [],
                    }),
                },
            },
        };
        await act(async () => {
            await hydrateCompanionReaderState();
        });

        await waitFor(() => expect(document.querySelector("[data-color='green']")).toBeTruthy());
        expect(await screen.findByRole("button", { name: "译文" })).toBeTruthy();
    });

    it("clears a highlight from the right-click menu", async () => {
        pdfWithPages(1);
        savePdfMarks("C:/docs/a.pdf", [{
            id: "m-clear",
            page: 1,
            color: "blue",
            rects: [{ x: 0, y: 0, w: 1, h: 1 }],
            text: "Hello",
        }]);
        render(<CompanionPdfReader absPath="C:/docs/a.pdf" theme={theme} lang="zh-Hans" />);
        const page = await screen.findByTestId("companion-pdf-page-1");
        box(page, 200, 400);
        vi.spyOn(window, "getSelection").mockReturnValue({ isCollapsed: true, rangeCount: 0, toString: () => "", removeAllRanges() {} } as unknown as Selection);
        fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });
        expect(screen.queryByTestId("pdf-translate")).toBeNull();
        fireEvent.mouseDown(screen.getByTestId("pdf-highlight-clear"));
        expect(document.querySelector("[data-color='blue']")).toBeNull();
        expect(loadPdfMarks("C:/docs/a.pdf").some((item) => item.id === "m-clear")).toBe(false);
    });

    it("does not paint every page of a long file up front", async () => {
        const getPage = pdfWithPages(3, "OnlyOne");
        render(<CompanionPdfReader absPath="C:/docs/long.pdf" theme={theme} lang="zh-Hans" />);
        expect(await screen.findByText("OnlyOne")).toBeTruthy();
        expect(screen.getByTestId("companion-pdf-page-3")).toBeTruthy();
        expect(getPage).toHaveBeenCalledWith(1);
        expect(getPage).not.toHaveBeenCalledWith(3);
    });

    it("shows the current page and total above the PDF and jumps without scrolling the window", async () => {
        const getPage = pdfWithPages(5, "OnlyOne");
        const scrolled = vi.spyOn(Element.prototype, "scrollIntoView");
        render(<CompanionPdfReader absPath="C:/docs/long.pdf" theme={theme} lang="zh-Hans" />);
        expect(await screen.findByText("OnlyOne")).toBeTruthy();

        const pager = screen.getByTestId("companion-pdf-pager");
        const scroller = screen.getByTestId("companion-pdf-scroll");
        expect(pager.getAttribute("aria-label")).toBe("PDF 页码，第 1 页，共 5 页");
        expect(pager.textContent).toContain("/ 5 页");
        expect(scroller.contains(pager)).toBe(false);
        expect(pager.compareDocumentPosition(scroller) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect(pager.compareDocumentPosition(screen.getByTestId("pdf-draw")) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        const input = screen.getByTestId("companion-pdf-goto") as HTMLInputElement;
        expect(input.value).toBe("1");
        expect((screen.getByTestId("companion-pdf-page-prev") as HTMLButtonElement).disabled).toBe(true);
        expect(getPage).not.toHaveBeenCalledWith(3);

        scroller.getBoundingClientRect = () => clientRect(0, 40, 300, 400);
        const page3 = screen.getByTestId("companion-pdf-page-3");
        page3.getBoundingClientRect = () => clientRect(0, 900 - scroller.scrollTop, 300, 400) as DOMRect;
        fireEvent.change(input, { target: { value: "3a" } });
        expect(input.value).toBe("3");
        fireEvent.submit(pager);
        expect(input.value).toBe("3");
        expect(scroller.scrollTop).toBe(860);
        expect(scrolled).not.toHaveBeenCalled();
        fireEvent.pointerDown(scroller);
        fireEvent.pointerUp(scroller);
        expect(scroller.scrollTop).toBe(860);
        expect(input.value).toBe("3");
        await waitFor(() => expect(getPage).toHaveBeenCalledWith(3));
        expect(getPage).not.toHaveBeenCalledWith(5);

        const next = screen.getByTestId("companion-pdf-page-next");
        fireEvent.mouseDown(next);
        fireEvent.click(next, { detail: 1 });
        expect((screen.getByTestId("companion-pdf-goto") as HTMLInputElement).value).toBe("4");
        fireEvent.click(next, { detail: 0 });
        expect((screen.getByTestId("companion-pdf-goto") as HTMLInputElement).value).toBe("5");
        fireEvent.mouseDown(screen.getByTestId("companion-pdf-page-prev"));
        expect((screen.getByTestId("companion-pdf-goto") as HTMLInputElement).value).toBe("4");
        fireEvent.change(screen.getByTestId("companion-pdf-goto"), { target: { value: "1" } });
        fireEvent.mouseDown(screen.getByTestId("companion-pdf-page-next"));
        expect((screen.getByTestId("companion-pdf-goto") as HTMLInputElement).value).toBe("5");
        expect((screen.getByTestId("companion-pdf-page-next") as HTMLButtonElement).disabled).toBe(true);
        fireEvent.mouseDown(screen.getByTestId("companion-pdf-page-prev"));
        expect((screen.getByTestId("companion-pdf-goto") as HTMLInputElement).value).toBe("4");
        fireEvent.change(screen.getByTestId("companion-pdf-goto"), { target: { value: "2" } });
        fireEvent.mouseDown(screen.getByTestId("companion-pdf-goto-go"));
        expect((screen.getByTestId("companion-pdf-goto") as HTMLInputElement).value).toBe("2");
        fireEvent.change(screen.getByTestId("companion-pdf-goto"), { target: { value: "99" } });
        fireEvent.mouseDown(screen.getByTestId("companion-pdf-goto-go"));
        expect((screen.getByTestId("companion-pdf-goto") as HTMLInputElement).value).toBe("5");
        expect((screen.getByTestId("companion-pdf-page-next") as HTMLButtonElement).disabled).toBe(true);
        await waitFor(() => expect(getPage).toHaveBeenCalledWith(5));

        fireEvent.change(screen.getByTestId("companion-pdf-goto"), { target: { value: "" } });
        fireEvent.submit(screen.getByTestId("companion-pdf-pager"));
        expect((screen.getByTestId("companion-pdf-goto") as HTMLInputElement).value).toBe("5");
    });

    it("follows the page at the top of the reader while scrolling and keeps a typed page", async () => {
        pdfWithPages(3, "OnlyOne");
        render(<CompanionPdfReader absPath="C:/docs/long.pdf" theme={theme} lang="zh-Hans" />);
        await screen.findByText("OnlyOne");
        const scroller = screen.getByTestId("companion-pdf-scroll");
        Object.defineProperty(scroller, "clientHeight", { configurable: true, get: () => 400 });
        scroller.getBoundingClientRect = () => clientRect(0, 0, 300, 400);
        const rects = [
            clientRect(0, -400, 300, 390),
            clientRect(0, 0, 300, 390),
            clientRect(0, 400, 300, 390),
        ];
        for (let page = 1; page <= 3; page += 1) {
            screen.getByTestId(`companion-pdf-page-${page}`).getBoundingClientRect = () => rects[page - 1] as DOMRect;
        }
        await act(async () => {
            fireEvent.scroll(scroller);
            await new Promise((resolve) => requestAnimationFrame(resolve));
        });
        const input = screen.getByTestId("companion-pdf-goto") as HTMLInputElement;
        expect(input.value).toBe("2");

        fireEvent.focus(input);
        fireEvent.change(input, { target: { value: "3" } });
        rects[1] = clientRect(0, -20, 300, 390);
        await act(async () => {
            fireEvent.scroll(scroller);
            await new Promise((resolve) => requestAnimationFrame(resolve));
        });
        expect(input.value).toBe("3");
        fireEvent.blur(input);
        expect(input.value).toBe("3");
        expect(screen.getByTestId("companion-pdf-pager").getAttribute("aria-label")).toContain("第 3 页");

        input.focus();
        fireEvent.change(input, { target: { value: "" } });
        fireEvent.submit(screen.getByTestId("companion-pdf-pager"));
        expect(input.value).toBe("3");
        rects[1] = clientRect(0, 0, 300, 390);
        rects[2] = clientRect(0, 400, 300, 390);
        await act(async () => {
            fireEvent.scroll(scroller);
            await new Promise((resolve) => requestAnimationFrame(resolve));
        });
        expect(input.value).toBe("3");
        fireEvent.blur(input);
        expect(input.value).toBe("2");
    });

    it("turns the page from what is on screen when the label is a frame behind", async () => {
        pdfWithPages(3, "OnlyOne");
        render(<CompanionPdfReader absPath="C:/docs/long.pdf" theme={theme} lang="zh-Hans" />);
        await screen.findByText("OnlyOne");
        const scroller = screen.getByTestId("companion-pdf-scroll");
        Object.defineProperty(scroller, "clientHeight", { configurable: true, get: () => 400 });
        scroller.getBoundingClientRect = () => clientRect(0, 0, 300, 400);
        const tops = [-400, 0, 400];
        for (let page = 1; page <= 3; page += 1) {
            const top = tops[page - 1];
            screen.getByTestId(`companion-pdf-page-${page}`).getBoundingClientRect = () => clientRect(0, top - scroller.scrollTop, 300, 390) as DOMRect;
        }
        expect((screen.getByTestId("companion-pdf-goto") as HTMLInputElement).value).toBe("1");
        fireEvent.mouseDown(screen.getByTestId("companion-pdf-page-next"));
        expect((screen.getByTestId("companion-pdf-goto") as HTMLInputElement).value).toBe("3");
    });

    it("holds a jumped page while its height changes and keeps a later scroll", async () => {
        const callbacks: ResizeObserverCallback[] = [];
        class MockResizeObserver {
            constructor(cb: ResizeObserverCallback) { callbacks.push(cb); }
            observe() {}
            unobserve() {}
            disconnect() {}
        }
        const prior = globalThis.ResizeObserver;
        vi.stubGlobal("ResizeObserver", MockResizeObserver);
        try {
            pdfWithPages(5, "OnlyOne");
            render(<CompanionPdfReader absPath="C:/docs/long.pdf" theme={theme} lang="zh-Hans" />);
            await screen.findByText("OnlyOne");
            const scroller = screen.getByTestId("companion-pdf-scroll");
            scroller.getBoundingClientRect = () => clientRect(0, 40, 300, 400);
            let shift = 0;
            const page3 = screen.getByTestId("companion-pdf-page-3");
            page3.getBoundingClientRect = () => clientRect(0, 900 - scroller.scrollTop + shift, 300, 400) as DOMRect;
            fireEvent.change(screen.getByTestId("companion-pdf-goto"), { target: { value: "3" } });
            fireEvent.submit(screen.getByTestId("companion-pdf-pager"));
            expect(scroller.scrollTop).toBe(860);

            shift = 120;
            act(() => { callbacks.forEach((cb) => cb([], {} as ResizeObserver)); });
            expect(scroller.scrollTop).toBe(980);

            fireEvent.pointerDown(scroller);
            fireEvent.pointerUp(scroller);
            shift = 200;
            act(() => { callbacks.forEach((cb) => cb([], {} as ResizeObserver)); });
            expect(scroller.scrollTop).toBe(1060);

            // A wheel that does not move the PDF, such as one over the translation
            // card, must not drop the hold. The card has its own scroller.
            fireEvent.wheel(scroller, { deltaY: 40 });
            shift = 280;
            act(() => { callbacks.forEach((cb) => cb([], {} as ResizeObserver)); });
            expect(scroller.scrollTop).toBe(1140);

            scroller.scrollTop += 40;
            fireEvent.scroll(scroller);
            shift = 400;
            act(() => { callbacks.forEach((cb) => cb([], {} as ResizeObserver)); });
            expect(scroller.scrollTop).toBe(1180);
        } finally {
            if (prior) vi.stubGlobal("ResizeObserver", prior);
            else Reflect.deleteProperty(globalThis, "ResizeObserver");
        }
    });

    it("translates only the selected text and keeps the Chinese popover", async () => {
        pdfWithPages(1);
        let release: (value: string) => void = () => undefined;
        translate.mockImplementation(() => new Promise<string>((resolve) => {
            release = resolve;
        }));
        const view = render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const page = await screen.findByTestId("companion-pdf-page-1");
        selectHello(page);
        fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });
        expect(screen.getByTestId("pdf-highlight-menu").textContent).toContain("高亮");
        expect(screen.getByTestId("pdf-translate").querySelector("svg")).toBeTruthy();
        const marksBefore = JSON.stringify(loadPdfMarks("C:/docs/Paper.PDF"));
        fireEvent.mouseDown(screen.getByTestId("pdf-translate"));
        expect(translate).toHaveBeenCalledTimes(1);
        expect(translate).toHaveBeenCalledWith("C:/docs/Paper.PDF", "Hello");
        expect(document.querySelector(".companion-pdf-translation")).toBeTruthy();
        expect(screen.getByRole("button", { name: "正在翻译" }).getAttribute("aria-busy")).toBe("true");
        expect(screen.getByRole("button", { name: "正在翻译" }).textContent).toBe("译文");

        fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });
        fireEvent.mouseDown(screen.getByTestId("pdf-translate"));
        expect(translate).toHaveBeenCalledTimes(1);
        expect(document.querySelectorAll(".companion-pdf-translation")).toHaveLength(1);

        release("你好");
        const icon = await screen.findByRole("button", { name: "译文" });
        expect(icon.textContent).toBe("译文");
        expect(document.querySelectorAll(".companion-pdf-translation")).toHaveLength(1);
        expect((document.querySelector(".companion-pdf-translation") as HTMLElement).style.left).toBe("5%");
        expect(JSON.stringify(loadPdfMarks("C:/docs/Paper.PDF"))).toBe(marksBefore);
        const saved = loadPdfTranslations("C:/docs/Paper.PDF");
        expect(saved.some((item) => item.translation === "你好")).toBe(true);
        expect(saved.some((item) => item.text === "Hello")).toBe(true);
        fireEvent.mouseDown(icon);
        expect(screen.getByRole("dialog").textContent).toContain("你好");
        expect(screen.getByRole("dialog").className).not.toContain("companion-pdf-translation-pop--above");
        expect(page.style.getPropertyValue("--page-w")).toBe("200px");
        expect(Number.parseFloat((document.querySelector(".companion-pdf-translation") as HTMLElement).style.getPropertyValue("--sel-x"))).toBeCloseTo(10, 3);
        const popRule = readFileSync(resolve("src/styles/partials/110-mc-app-shell.css"), "utf8");
        expect(popRule).toMatch(/\.companion-pdf-translation-pop\s*\{[^}]*box-sizing:\s*border-box/);
        expect(popRule).toMatch(/\.companion-pdf-translation-pop\s*\{[^}]*translate\(calc\(var\(--pop-x, 0px\) \+ min\(0px, calc\(var\(--page-w, 280px\) - 100% - var\(--sel-x, 0px\)\)\)\), var\(--pop-y, 0px\)\)/);

        view.unmount();
        const again = render(<CompanionPdfReader absPath="c:/docs/paper.pdf" theme={theme} lang="zh-Hans" />);
        expect(await screen.findByRole("button", { name: "译文" })).toBeTruthy();
        again.unmount();

        render(<CompanionPdfReader absPath="C:/docs/other.pdf" theme={theme} lang="zh-Hans" />);
        await screen.findByTestId("companion-pdf-page-1");
        expect(screen.queryByRole("button", { name: "译文" })).toBeNull();
    });

    it("opens the translation above the line when that line is at the bottom of the reader", async () => {
        pdfWithPages(1);
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const page = await screen.findByTestId("companion-pdf-page-1");
        selectHello(page);
        fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });
        fireEvent.mouseDown(screen.getByTestId("pdf-translate"));
        const icon = await screen.findByRole("button", { name: "译文" });
        const orig = HTMLElement.prototype.getBoundingClientRect;
        HTMLElement.prototype.getBoundingClientRect = function () {
            if (this.hasAttribute("data-pdf-translation-pop")) return clientRect(10, 206, 120, 150);
            if (this.classList.contains("companion-pdf-translation")) return clientRect(10, 180, 40, 20);
            if (this.dataset.testid === "companion-pdf-scroll") return clientRect(0, 0, 224, 220);
            return orig.call(this);
        };
        try {
            fireEvent.mouseDown(icon);
            const pop = screen.getByRole("dialog");
            expect(pop.className).toContain("companion-pdf-translation-pop--above");
            expect(pop.textContent).toContain("你好");
            const css = readFileSync(resolve("src/styles/partials/110-mc-app-shell.css"), "utf8");
            expect(css).toMatch(/\.companion-pdf-translation-pop--above\s*\{[^}]*bottom:\s*calc\(100% \+ 12px\)/);
        } finally {
            HTMLElement.prototype.getBoundingClientRect = orig;
        }
    });

    it("scrolls the reader when the translation fits on neither side of the line", async () => {
        pdfWithPages(1);
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const page = await screen.findByTestId("companion-pdf-page-1");
        selectHello(page);
        fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });
        fireEvent.mouseDown(screen.getByTestId("pdf-translate"));
        const icon = await screen.findByRole("button", { name: "译文" });
        const scroller = screen.getByTestId("companion-pdf-scroll");
        const orig = HTMLElement.prototype.getBoundingClientRect;
        HTMLElement.prototype.getBoundingClientRect = function () {
            if (this.hasAttribute("data-pdf-translation-pop")) return clientRect(10, 126, 120, 200);
            if (this.classList.contains("companion-pdf-translation")) return clientRect(10, 100, 40, 20);
            if (this.dataset.testid === "companion-pdf-scroll") return clientRect(0, 0, 224, 180);
            return orig.call(this);
        };
        try {
            fireEvent.mouseDown(icon);
            expect(screen.getByRole("dialog").className).not.toContain("companion-pdf-translation-pop--above");
            expect(scroller.scrollTop).toBe(146);
        } finally {
            HTMLElement.prototype.getBoundingClientRect = orig;
        }
    });

    it("sends a long selection once and does not stop at the highlight limit", async () => {
        pdfWithPages(1, "PAGE_TEXT_SHOULD_NOT_BE_SENT");
        const selected = "乙".repeat(4001);
        render(<CompanionPdfReader absPath="C:/docs/long.pdf" theme={theme} lang="zh-Hans" />);
        const page = await screen.findByTestId("companion-pdf-page-1");
        box(page, 200, 400);
        const rect = { left: 10, top: 20, right: 50, bottom: 32, width: 40, height: 12 };
        vi.spyOn(window, "getSelection").mockReturnValue({
            isCollapsed: false,
            rangeCount: 1,
            toString: () => selected,
            removeAllRanges: () => undefined,
            getRangeAt: () => ({ getClientRects: () => [rect] }),
        } as unknown as Selection);
        fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });
        fireEvent.mouseDown(screen.getByTestId("pdf-translate"));
        expect(translate).toHaveBeenCalledTimes(1);
        const sent = translate.mock.calls[0][1] as string;
        expect(sent).toBe("乙".repeat(4000));
        expect(sent).not.toContain("PAGE_TEXT_SHOULD_NOT_BE_SENT");
        expect(sent).not.toContain("C:/docs/long.pdf");
        const icon = await screen.findByRole("button", { name: "译文" });
        fireEvent.mouseDown(icon);
        const pop = screen.getByRole("dialog");
        expect(pop.textContent).toContain("你好");
        expect(pop.querySelector(".companion-pdf-translation-body")?.firstElementChild?.textContent).toContain("仅翻译了前 4000 字");
    });

    it("saves a dragged translation popover and restores that offset", async () => {
        pdfWithPages(1);
        const view = render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const page = await screen.findByTestId("companion-pdf-page-1");
        selectHello(page);
        fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });
        fireEvent.mouseDown(screen.getByTestId("pdf-translate"));
        const icon = await screen.findByRole("button", { name: "译文" });
        fireEvent.mouseDown(icon);
        const pop = screen.getByRole("dialog");
        expect(pop.textContent).toContain("你好");
        const grip = pop.querySelector("[data-pdf-translation-grip]") as HTMLElement;
        expect(grip.getAttribute("title")).toBe("拖动");
        const css = readFileSync(resolve("src/styles/partials/110-mc-app-shell.css"), "utf8");
        expect(css).toMatch(/\.companion-pdf-translation-grip\s*\{[^}]*cursor:\s*grab/);
        expect(css).toMatch(/\.companion-pdf-translation-chrome\s*\{[^}]*position:\s*sticky/);
        fireEvent.mouseDown(grip, { button: 0, clientX: 0, clientY: 0 });
        fireEvent.mouseUp(window);
        expect(loadPdfTranslations("C:/docs/Paper.PDF").some((item) => item.place)).toBe(false);
        const copy = pop.querySelector("[data-testid^='pdf-translate-copy-']") as HTMLElement;
        fireEvent.mouseDown(copy, { button: 0, clientX: 0, clientY: 0 });
        fireEvent.mouseMove(window, { clientX: 40, clientY: 24 });
        fireEvent.mouseUp(window);
        expect(document.body.classList.contains("companion-pdf-translation-dragging")).toBe(false);
        expect(loadPdfTranslations("C:/docs/Paper.PDF").some((item) => item.place)).toBe(false);

        const kicker = pop.querySelector(".companion-pdf-translation-kicker") as HTMLElement;
        fireEvent.mouseDown(kicker, { button: 0, clientX: 0, clientY: 0 });
        fireEvent.mouseMove(window, { clientX: 30, clientY: 20 });
        fireEvent.mouseUp(window);
        const span = document.querySelector(".companion-pdf-translation") as HTMLElement;
        expect(Number.parseFloat(span.style.getPropertyValue("--pop-x"))).toBeCloseTo(30, 3);
        expect(Number.parseFloat(span.style.getPropertyValue("--pop-y"))).toBeCloseTo(20, 3);
        const row = loadPdfTranslations("C:/docs/Paper.PDF")[0];
        expect(row?.translation).toBe("你好");
        expect(row?.place?.x).toBeCloseTo(0.15, 4);
        expect(row?.place?.y).toBeCloseTo(0.05, 4);
        expect(row?.place?.above).toBeUndefined();
        expect(pop.textContent).toContain("你好");
        view.unmount();

        render(<CompanionPdfReader absPath="c:/docs/paper.pdf" theme={theme} lang="zh-Hans" />);
        const again = await screen.findByTestId("companion-pdf-page-1");
        box(again, 200, 400);
        const restored = document.querySelector(".companion-pdf-translation") as HTMLElement;
        expect(Number.parseFloat(restored.style.getPropertyValue("--pop-x"))).toBeCloseTo(30, 3);
        expect(Number.parseFloat(restored.style.getPropertyValue("--pop-y"))).toBeCloseTo(20, 3);
        const orig = HTMLElement.prototype.getBoundingClientRect;
        const restoredScroller = screen.getByTestId("companion-pdf-scroll");
        Object.defineProperty(restoredScroller, "clientHeight", { configurable: true, value: 180 });
        HTMLElement.prototype.getBoundingClientRect = function () {
            if (this.hasAttribute("data-pdf-translation-pop")) return clientRect(10, 126, 120, 200);
            if (this.classList.contains("companion-pdf-translation")) return clientRect(10, 100, 40, 20);
            if (this.dataset.testid === "companion-pdf-scroll") return clientRect(0, 0, 224, 180);
            return orig.call(this);
        };
        try {
            fireEvent.mouseDown(screen.getByRole("button", { name: "译文" }));
            const opened = screen.getByRole("dialog");
            expect(opened.className).not.toContain("companion-pdf-translation-pop--above");
            expect(opened.textContent).toContain("你好");
            expect(screen.getByTestId("companion-pdf-scroll").scrollTop).toBe(100);
        } finally {
            HTMLElement.prototype.getBoundingClientRect = orig;
        }
    });

    it("keeps a dragged translation inside the reader", async () => {
        pdfWithPages(1);
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const page = await screen.findByTestId("companion-pdf-page-1");
        selectHello(page);
        fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });
        fireEvent.mouseDown(screen.getByTestId("pdf-translate"));
        const icon = await screen.findByRole("button", { name: "译文" });
        fireEvent.mouseDown(icon);
        const pop = screen.getByRole("dialog");
        const scroller = screen.getByTestId("companion-pdf-scroll");
        page.getBoundingClientRect = () => clientRect(0, 0, 200, 400);
        scroller.getBoundingClientRect = () => clientRect(0, 0, 180, 220);
        Object.defineProperty(scroller, "clientWidth", { configurable: true, value: 180 });
        Object.defineProperty(scroller, "clientHeight", { configurable: true, value: 220 });
        pop.getBoundingClientRect = () => {
            const host = pop.parentElement as HTMLElement;
            const x = Number.parseFloat(host.style.getPropertyValue("--pop-x")) || 0;
            const y = Number.parseFloat(host.style.getPropertyValue("--pop-y")) || 0;
            return clientRect(10 + x, 40 + y, 120, 80);
        };
        const grip = pop.querySelector("[data-pdf-translation-grip]") as HTMLElement;
        const css = readFileSync(resolve("src/styles/partials/110-mc-app-shell.css"), "utf8");
        expect(css).toMatch(/body\.companion-pdf-translation-dragging \* \{[^}]*cursor:\s*grabbing/);
        fireEvent.mouseDown(grip, { button: 0, clientX: 0, clientY: 0 });
        expect(document.body.classList.contains("companion-pdf-translation-dragging")).toBe(true);
        fireEvent.mouseMove(window, { clientX: 100, clientY: 200 });
        fireEvent.blur(window);
        expect(document.body.classList.contains("companion-pdf-translation-dragging")).toBe(false);
        const span = pop.parentElement as HTMLElement;
        expect(Number.parseFloat(span.style.getPropertyValue("--pop-x"))).toBeCloseTo(50, 3);
        expect(Number.parseFloat(span.style.getPropertyValue("--pop-y"))).toBeCloseTo(100, 3);
        const row = loadPdfTranslations("C:/docs/Paper.PDF")[0];
        expect(row?.place?.x).toBeCloseTo(0.25, 4);
        expect(row?.place?.y).toBeCloseTo(0.25, 4);
    });

    it("writes a selection annotation as a shareable note card", async () => {
        pdfWithPages(1);
        const changed = vi.fn();
        window.addEventListener(PDF_ANNOTATIONS_CHANGED_EVENT, changed);
        try {
            render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
            const page = await screen.findByTestId("companion-pdf-page-1");
            selectHello(page);
            fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });
            expect(screen.queryByTestId("pdf-annotation-input")).toBeNull();
            fireEvent.mouseDown(screen.getByTestId("pdf-annotate"));
            const editor = screen.getByTestId("pdf-annotation-input") as HTMLTextAreaElement;
            fireEvent.keyDown(editor, { key: "Escape" });
            expect(screen.queryByTestId("pdf-annotation-input")).toBeNull();

            fireEvent.mouseDown(screen.getByTestId("pdf-annotate"));
            fireEvent.change(screen.getByTestId("pdf-annotation-input"), { target: { value: "这里的假设需要检验" } });
            fireEvent.click(screen.getByTestId("pdf-annotation-save"));
            // An annotation is written locally; it is not a model request.
            expect(translate).not.toHaveBeenCalled();
            expect(changed).toHaveBeenCalledTimes(1);
            const icon = screen.getByRole("button", { name: "批注" });
            expect(icon.textContent).toBe("批注");
            const pop = screen.getByRole("dialog");
            expect(pop.getAttribute("aria-label")).toBe("批注");
            expect(pop.querySelector(".companion-pdf-translation-kicker")?.textContent).toBe("批注");
            expect(pop.textContent).toContain("这里的假设需要检验");
            expect(pop.textContent).toContain("Hello");
            const saved = loadPdfTranslations("C:/docs/Paper.PDF");
            expect(saved).toHaveLength(1);
            expect(saved[0]).toMatchObject({ kind: "annotation", text: "Hello", translation: "这里的假设需要检验" });

            // Right-clicking the card names it an annotation, not a translation.
            fireEvent.contextMenu(icon, { clientX: 20, clientY: 30 });
            expect(screen.getByTestId("pdf-translate-clear").textContent).toContain("删除批注");
            fireEvent.mouseDown(screen.getByTestId("pdf-translate-clear"));
            expect(screen.queryByRole("dialog")).toBeNull();
            expect(loadPdfTranslations("C:/docs/Paper.PDF")).toEqual([]);
            expect(changed).toHaveBeenCalledTimes(2);
        } finally {
            window.removeEventListener(PDF_ANNOTATIONS_CHANGED_EVENT, changed);
        }
    });

    it("resizes a translation card and restores its saved size on remount", async () => {
        pdfWithPages(1, "Hello world", { width: 800, height: 1000 });
        const changed = vi.fn();
        window.addEventListener(PDF_ANNOTATIONS_CHANGED_EVENT, changed);
        try {
            const view = render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
            const page = await screen.findByTestId("companion-pdf-page-1");
            selectHello(page);
            fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });
            fireEvent.mouseDown(screen.getByTestId("pdf-translate"));
            const icon = await screen.findByRole("button", { name: "译文" });
            fireEvent.mouseDown(icon);
            const pop = screen.getByRole("dialog");
            box(page, 800, 1000);
            // The card can grow but never shrink below the 200x100 minimum.
            Object.defineProperty(pop, "offsetWidth", { configurable: true, value: 240 });
            Object.defineProperty(pop, "offsetHeight", { configurable: true, value: 90 });
            const handle = pop.querySelector("[data-pdf-translation-resize]") as HTMLElement;
            expect(handle.getAttribute("title")).toBe("调整大小");
            fireEvent.mouseDown(handle, { button: 0, clientX: 0, clientY: 0 });
            fireEvent.mouseMove(window, { clientX: 40, clientY: 30 });
            fireEvent.mouseUp(window);
            expect(pop.style.width).toBe("280px");
            expect(pop.style.height).toBe("120px");
            const row = loadPdfTranslations("C:/docs/Paper.PDF")[0];
            expect(row?.kind).toBeUndefined();
            expect(row?.size?.w).toBeCloseTo(0.35, 4);
            expect(row?.size?.h).toBeCloseTo(0.12, 4);
            // Translations stay local; only annotations travel with the share.
            expect(changed).not.toHaveBeenCalled();

            view.unmount();
            render(<CompanionPdfReader absPath="c:/docs/paper.pdf" theme={theme} lang="zh-Hans" />);
            await screen.findByTestId("companion-pdf-page-1");
            fireEvent.mouseDown(screen.getByRole("button", { name: "译文" }));
            const restored = screen.getByRole("dialog");
            expect(restored.style.width).toBe("280px");
            expect(restored.style.height).toBe("120px");
        } finally {
            window.removeEventListener(PDF_ANNOTATIONS_CHANGED_EVENT, changed);
        }
    });

    it("resizes an annotation card and announces the change for the share", async () => {
        pdfWithPages(1, "Hello world", { width: 800, height: 1000 });
        const changed = vi.fn();
        window.addEventListener(PDF_ANNOTATIONS_CHANGED_EVENT, changed);
        try {
            render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
            const page = await screen.findByTestId("companion-pdf-page-1");
            selectHello(page);
            fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });
            fireEvent.mouseDown(screen.getByTestId("pdf-annotate"));
            fireEvent.change(screen.getByTestId("pdf-annotation-input"), { target: { value: "复核这段推导" } });
            fireEvent.click(screen.getByTestId("pdf-annotation-save"));
            expect(changed).toHaveBeenCalledTimes(1);
            const pop = screen.getByRole("dialog");
            box(page, 800, 1000);
            Object.defineProperty(pop, "offsetWidth", { configurable: true, value: 240 });
            Object.defineProperty(pop, "offsetHeight", { configurable: true, value: 90 });
            const handle = pop.querySelector("[data-pdf-translation-resize]") as HTMLElement;
            fireEvent.mouseDown(handle, { button: 0, clientX: 0, clientY: 0 });
            fireEvent.mouseMove(window, { clientX: 40, clientY: 30 });
            fireEvent.mouseUp(window);
            expect(pop.style.width).toBe("280px");
            expect(pop.style.height).toBe("120px");
            const saved = loadPdfTranslations("C:/docs/Paper.PDF")[0];
            expect(saved).toMatchObject({ kind: "annotation", translation: "复核这段推导" });
            expect(saved?.size?.w).toBeCloseTo(0.35, 4);
            expect(saved?.size?.h).toBeCloseTo(0.12, 4);
            expect(changed).toHaveBeenCalledTimes(2);
        } finally {
            window.removeEventListener(PDF_ANNOTATIONS_CHANGED_EVENT, changed);
        }
    });

    it("draws a freehand line only while 手绘 is pressed", async () => {
        pdfWithPages(1);
        const changed = vi.fn();
        window.addEventListener(PDF_DRAWINGS_CHANGED_EVENT, changed);
        try {
            const view = render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
            const page = await screen.findByTestId("companion-pdf-page-1");
            box(page, 200, 400);
            const toggle = screen.getByTestId("pdf-draw-toggle");
            expect(toggle.getAttribute("aria-pressed")).toBe("false");
            expect(screen.queryByTestId("pdf-draw-panel")).toBeNull();
            fireEvent.click(toggle);
            expect(toggle.getAttribute("aria-pressed")).toBe("true");
            expect(screen.getByTestId("pdf-draw-panel")).toBeTruthy();
            expect(screen.getByTestId("pdf-draw-line").getAttribute("aria-pressed")).toBe("true");
            expect(screen.getByTestId("pdf-draw-color-yellow").getAttribute("aria-pressed")).toBe("true");
            expect((screen.getByTestId("pdf-draw-undo") as HTMLButtonElement).disabled).toBe(true);

            const layer = screen.getByTestId("pdf-drawing-layer-1");
            expect(layer.getAttribute("class")).toContain("--active");
            expect(layer.getAttribute("class")).toContain("companion-pdf-drawings--pen");
            const drawCss = readFileSync(resolve("src/styles/partials/110-mc-app-shell.css"), "utf8");
            expect(drawCss).toMatch(/\.companion-pdf-drawings--active\s*\{[^}]*cursor:\s*crosshair/);
            expect(drawCss).toMatch(/\.companion-pdf-drawings--pen\s*\{[^}]*cursor:\s*url\([^)]*\)\s*4\s+28,\s*crosshair/);
            box(layer, 200, 400);
            fireEvent.mouseDown(layer, { button: 0, clientX: 10, clientY: 20 });
            fireEvent.mouseMove(window, { clientX: 60, clientY: 80 });
            fireEvent.mouseMove(window, { clientX: 110, clientY: 160 });
            fireEvent.mouseUp(window);
            const saved = loadPdfDrawings("C:/docs/Paper.PDF");
            expect(saved).toHaveLength(1);
            expect(saved[0]).toMatchObject({ page: 1, tool: "line", color: "yellow" });
            expect(saved[0].points[0]).toEqual({ x: 0.05, y: 0.05 });
            expect(saved[0].points[saved[0].points.length - 1]).toEqual({ x: 0.55, y: 0.4 });
            expect(saved[0].points.length).toBeGreaterThanOrEqual(3);
            expect(changed).toHaveBeenCalledTimes(1);
            expect(screen.getByTestId("pdf-drawing-layer-1").querySelector("polyline[data-drawing-id]")).toBeTruthy();

            // The same button leaves drawing mode; a press no longer paints.
            fireEvent.click(toggle);
            expect(toggle.getAttribute("aria-pressed")).toBe("false");
            expect(screen.queryByTestId("pdf-draw-panel")).toBeNull();
            fireEvent.mouseDown(layer, { button: 0, clientX: 20, clientY: 30 });
            fireEvent.mouseMove(window, { clientX: 90, clientY: 100 });
            fireEvent.mouseUp(window);
            expect(loadPdfDrawings("C:/docs/Paper.PDF")).toHaveLength(1);

            view.unmount();
            render(<CompanionPdfReader absPath="c:/docs/paper.pdf" theme={theme} lang="zh-Hans" />);
            await screen.findByTestId("companion-pdf-page-1");
            expect(screen.getByTestId("pdf-drawing-layer-1").querySelector("polyline[data-drawing-id]")).toBeTruthy();
            // 撤销 removes the newest stroke and is itself shareable state.
            fireEvent.click(screen.getByTestId("pdf-draw-toggle"));
            fireEvent.click(screen.getByTestId("pdf-draw-undo"));
            expect(loadPdfDrawings("C:/docs/Paper.PDF")).toEqual([]);
            expect(screen.getByTestId("pdf-drawing-layer-1").querySelector("polyline[data-drawing-id]")).toBeNull();
        } finally {
            window.removeEventListener(PDF_DRAWINGS_CHANGED_EVENT, changed);
        }
    });

    it("draws a box in the selected colour and tool", async () => {
        pdfWithPages(1);
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const page = await screen.findByTestId("companion-pdf-page-1");
        box(page, 200, 400);
        fireEvent.click(screen.getByTestId("pdf-draw-toggle"));
        fireEvent.click(screen.getByTestId("pdf-draw-box"));
        fireEvent.click(screen.getByTestId("pdf-draw-color-blue"));
        expect(screen.getByTestId("pdf-draw-box").getAttribute("aria-pressed")).toBe("true");
        expect(screen.getByTestId("pdf-draw-color-blue").getAttribute("aria-pressed")).toBe("true");
        const layer = screen.getByTestId("pdf-drawing-layer-1");
        expect(layer.getAttribute("class")).toContain("companion-pdf-drawings--active");
        expect(layer.getAttribute("class")).not.toContain("companion-pdf-drawings--pen");
        box(layer, 200, 400);
        fireEvent.mouseDown(layer, { button: 0, clientX: 20, clientY: 40 });
        fireEvent.mouseMove(window, { clientX: 80, clientY: 100 });
        fireEvent.mouseMove(window, { clientX: 120, clientY: 140 });
        fireEvent.mouseUp(window);
        const saved = loadPdfDrawings("C:/docs/Paper.PDF");
        expect(saved).toHaveLength(1);
        expect(saved[0]).toMatchObject({ tool: "box", color: "blue", page: 1 });
        expect(saved[0].points).toEqual([{ x: 0.1, y: 0.1 }, { x: 0.6, y: 0.35 }]);
        const rect = layer.querySelector("rect[data-drawing-id]");
        expect(rect).toBeTruthy();
        expect(Number(rect?.getAttribute("x"))).toBeCloseTo(0.1, 4);
        expect(Number(rect?.getAttribute("y"))).toBeCloseTo(0.1, 4);
        expect(Number(rect?.getAttribute("width"))).toBeCloseTo(0.5, 4);
        expect(Number(rect?.getAttribute("height"))).toBeCloseTo(0.25, 4);
        expect(rect?.getAttribute("fill")).toBe("#b9d4f5");
        expect(rect?.getAttribute("stroke")).toBe("#2f6bbd");
        // Escape also leaves drawing mode.
        fireEvent.keyDown(window, { key: "Escape" });
        expect(screen.getByTestId("pdf-draw-toggle").getAttribute("aria-pressed")).toBe("false");
    });

    it("cancels an unfinished stroke when drawing is switched off", async () => {
        pdfWithPages(1);
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const page = await screen.findByTestId("companion-pdf-page-1");
        box(page, 200, 400);
        const toggle = screen.getByTestId("pdf-draw-toggle");
        fireEvent.click(toggle);
        const layer = screen.getByTestId("pdf-drawing-layer-1");
        box(layer, 200, 400);
        fireEvent.mouseDown(layer, { button: 0, clientX: 10, clientY: 20 });
        fireEvent.mouseMove(window, { clientX: 80, clientY: 90 });
        // The stroke has not been released yet; leaving the pen must not save it.
        fireEvent.click(toggle);
        expect(loadPdfDrawings("C:/docs/Paper.PDF")).toEqual([]);
        fireEvent.mouseUp(window);
        expect(loadPdfDrawings("C:/docs/Paper.PDF")).toEqual([]);
        expect(layer.querySelector("polyline[data-drawing-id]")).toBeNull();
    });

    it("drops an unfinished stroke when the tool changes", async () => {
        pdfWithPages(1);
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const page = await screen.findByTestId("companion-pdf-page-1");
        box(page, 200, 400);
        fireEvent.click(screen.getByTestId("pdf-draw-toggle"));
        const layer = screen.getByTestId("pdf-drawing-layer-1");
        box(layer, 200, 400);
        fireEvent.mouseDown(layer, { button: 0, clientX: 10, clientY: 20 });
        fireEvent.mouseMove(window, { clientX: 80, clientY: 100 });
        // The mouseup never arrived, but picking another tool abandons it.
        fireEvent.click(screen.getByTestId("pdf-draw-box"));
        fireEvent.mouseUp(window);
        expect(loadPdfDrawings("C:/docs/Paper.PDF")).toEqual([]);
        expect(layer.querySelector("polyline[data-drawing-id]")).toBeNull();
    });

    it("does not let a participant undo the host's drawing", async () => {
        pdfWithPages(1);
        hydratePdfDrawings({
            "c:/docs/paper.pdf": [{
                id: "d_host",
                page: 1,
                color: "blue",
                tool: "line",
                points: [{ x: 0.1, y: 0.2 }, { x: 0.3, y: 0.4 }],
                shared: true,
            }],
        });
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        await screen.findByTestId("companion-pdf-page-1");
        expect(screen.getByTestId("pdf-drawing-layer-1").querySelector("polyline[data-drawing-id]")).toBeTruthy();
        fireEvent.click(screen.getByTestId("pdf-draw-toggle"));
        expect((screen.getByTestId("pdf-draw-undo") as HTMLButtonElement).disabled).toBe(true);
    });

    it("erases local drawings inside a marquee and leaves host drawings", async () => {
        pdfWithPages(1);
        const lineMark = { id: "d_line", page: 1, color: "yellow" as const, tool: "line" as const, points: [{ x: 0.05, y: 0.05 }, { x: 0.25, y: 0.2 }] };
        const localBox = { id: "d_box", page: 1, color: "blue" as const, tool: "box" as const, points: [{ x: 0.6, y: 0.6 }, { x: 0.8, y: 0.8 }] };
        const hostBox = { id: "d_host", page: 1, color: "green" as const, tool: "box" as const, shared: true, points: [{ x: 0.1, y: 0.1 }, { x: 0.3, y: 0.3 }] };
        hydratePdfDrawings({ "c:/docs/paper.pdf": [lineMark, localBox, hostBox] });
        const changed = vi.fn();
        window.addEventListener(PDF_DRAWINGS_CHANGED_EVENT, changed);
        try {
            render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
            const page = await screen.findByTestId("companion-pdf-page-1");
            box(page, 200, 400);
            fireEvent.click(screen.getByTestId("pdf-draw-toggle"));
            fireEvent.click(screen.getByTestId("pdf-draw-eraser"));
            expect(screen.getByTestId("pdf-draw-eraser").getAttribute("aria-pressed")).toBe("true");
            expect(screen.getByTestId("pdf-draw-eraser").getAttribute("title")).toContain("擦除");
            expect(screen.getByTestId("pdf-draw-line").getAttribute("title")).toBe("画线");
            expect(screen.getByTestId("pdf-draw-undo").getAttribute("title")).toBe("撤销上一笔");
            expect((screen.getByTestId("pdf-draw-color-yellow") as HTMLButtonElement).disabled).toBe(true);
            const layer = screen.getByTestId("pdf-drawing-layer-1");
            expect(layer.getAttribute("class")).toContain("companion-pdf-drawings--eraser");
            expect(layer.getAttribute("class")).not.toContain("companion-pdf-drawings--pen");
            box(layer, 200, 400);

            // The marquee covers the local line and the host box, but not the
            // local box on the lower right.
            fireEvent.mouseDown(layer, { button: 0, clientX: 0, clientY: 0 });
            fireEvent.mouseMove(window, { clientX: 70, clientY: 130 });
            await waitFor(() => expect(screen.getByTestId("pdf-erase-preview-1")).toBeTruthy());
            await waitFor(() => expect(layer.querySelector("[data-drawing-id='d_line']")?.getAttribute("opacity")).toBe("0.35"));
            expect(layer.querySelector("[data-drawing-id='d_host']")?.getAttribute("opacity")).toBeNull();
            fireEvent.mouseUp(window);
            const saved = loadPdfDrawings("C:/docs/Paper.PDF").map((item) => item.id).sort();
            // The host's drawing is not ours to erase; the untouched box stays.
            expect(saved).toEqual(["d_box", "d_host"]);
            expect(changed).toHaveBeenCalledTimes(1);
            expect(screen.queryByTestId("pdf-erase-preview-1")).toBeNull();
            expect(layer.querySelector("[data-drawing-id='d_line']")).toBeNull();
            expect(layer.querySelector("[data-drawing-id='d_box']")).toBeTruthy();
            expect(layer.querySelector("[data-drawing-id='d_host']")).toBeTruthy();

            // An empty marquee, or a click without a drag, erases nothing.
            fireEvent.mouseDown(layer, { button: 0, clientX: 170, clientY: 20 });
            fireEvent.mouseMove(window, { clientX: 190, clientY: 60 });
            fireEvent.mouseUp(window);
            expect(loadPdfDrawings("C:/docs/Paper.PDF").map((item) => item.id).sort()).toEqual(["d_box", "d_host"]);
            expect(changed).toHaveBeenCalledTimes(1);
            fireEvent.mouseDown(layer, { button: 0, clientX: 130, clientY: 250 });
            fireEvent.mouseUp(window);
            expect(loadPdfDrawings("C:/docs/Paper.PDF").map((item) => item.id).sort()).toEqual(["d_box", "d_host"]);
            expect(changed).toHaveBeenCalledTimes(1);

            // The same button leaves the eraser and returns to the pen.
            fireEvent.click(screen.getByTestId("pdf-draw-eraser"));
            expect(screen.getByTestId("pdf-draw-eraser").getAttribute("aria-pressed")).toBe("false");
            expect(screen.getByTestId("pdf-draw-line").getAttribute("aria-pressed")).toBe("true");
            expect(screen.getByTestId("pdf-drawing-layer-1").getAttribute("class")).toContain("companion-pdf-drawings--pen");
            expect((screen.getByTestId("pdf-draw-color-yellow") as HTMLButtonElement).disabled).toBe(false);
        } finally {
            window.removeEventListener(PDF_DRAWINGS_CHANGED_EVENT, changed);
        }
    });

    it("keeps a dragged translation in place when the card re-renders", async () => {
        pdfWithPages(1);
        const writeText = vi.fn(async () => undefined);
        vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } });
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const page = await screen.findByTestId("companion-pdf-page-1");
        selectHello(page);
        fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });
        fireEvent.mouseDown(screen.getByTestId("pdf-translate"));
        const icon = await screen.findByRole("button", { name: "译文" });
        fireEvent.mouseDown(icon);
        const pop = screen.getByRole("dialog");
        const kicker = pop.querySelector(".companion-pdf-translation-kicker") as HTMLElement;
        fireEvent.mouseDown(kicker, { button: 0, clientX: 0, clientY: 0 });
        fireEvent.mouseMove(window, { clientX: 30, clientY: 20 });
        const span = pop.parentElement as HTMLElement;
        expect(Number.parseFloat(span.style.getPropertyValue("--pop-x"))).toBeCloseTo(30, 3);
        const copy = pop.querySelector("[data-testid^='pdf-translate-copy-']") as HTMLElement;
        await act(async () => {
            copy.dispatchEvent(new MouseEvent("click", { bubbles: true }));
            await Promise.resolve();
        });
        const livePop = screen.getByRole("dialog");
        expect(livePop.querySelector('[aria-label="已复制"]')).toBeTruthy();
        const live = livePop.parentElement as HTMLElement;
        expect(Number.parseFloat(live.style.getPropertyValue("--pop-x"))).toBeCloseTo(30, 3);
        expect(Number.parseFloat(live.style.getPropertyValue("--pop-y"))).toBeCloseTo(20, 3);
        fireEvent.mouseUp(window);
        expect(document.body.classList.contains("companion-pdf-translation-dragging")).toBe(false);
    });

    it("scrolls a saved translation into view when it sits fully below the reader", async () => {
        pdfWithPages(1);
        savePdfTranslations("C:/docs/Paper.PDF", [{
            id: "moved",
            text: "Hello",
            translation: "你好",
            place: { x: 0.1, y: 0.2 },
            parts: [{ page: 1, rects: [{ x: 0.05, y: 0.05, w: 0.2, h: 0.03 }] }],
        }]);
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const icon = await screen.findByRole("button", { name: "译文" });
        const scroller = screen.getByTestId("companion-pdf-scroll");
        // The border box is taller than the client box, as when a horizontal
        // scrollbar takes the bottom strip. The card scrolls to the client edge.
        Object.defineProperty(scroller, "clientHeight", { configurable: true, value: 200 });
        const orig = HTMLElement.prototype.getBoundingClientRect;
        HTMLElement.prototype.getBoundingClientRect = function () {
            if (this.hasAttribute("data-pdf-translation-pop")) return clientRect(10, 250, 120, 80);
            if (this.classList.contains("companion-pdf-translation")) return clientRect(10, 180, 40, 20);
            if (this.dataset.testid === "companion-pdf-scroll") return clientRect(0, 0, 224, 220);
            return orig.call(this);
        };
        try {
            fireEvent.mouseDown(icon);
            const opened = screen.getByRole("dialog");
            expect(opened.className).not.toContain("companion-pdf-translation-pop--above");
            expect(opened.textContent).toContain("你好");
            expect(scroller.scrollTop).toBe(130);
        } finally {
            HTMLElement.prototype.getBoundingClientRect = orig;
        }
    });

    it("drops a failed translation box and keeps the highlight", async () => {
        pdfWithPages(1);
        translate.mockRejectedValue(new Error("翻译失败，请稍后重试"));
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const page = await screen.findByTestId("companion-pdf-page-1");
        selectHello(page);
        fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });
        fireEvent.mouseDown(screen.getByTestId("pdf-highlight-green"));
        expect(document.querySelector("[data-color='green']")).toBeTruthy();
        fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });
        fireEvent.mouseDown(screen.getByTestId("pdf-translate"));
        expect((await screen.findByTestId("pdf-translate-error")).textContent).toContain("翻译失败，请稍后重试");
        await waitFor(() => expect(document.querySelector(".companion-pdf-translation")).toBeNull());
        expect(document.querySelector("[data-color='green']")).toBeTruthy();
        expect(loadPdfTranslations("C:/docs/Paper.PDF")).toHaveLength(0);
    });

    it("shows a translation that finishes after the same PDF is opened again", async () => {
        pdfWithPages(1);
        let release: (value: string) => void = () => undefined;
        translate.mockImplementation(() => new Promise<string>((resolve) => {
            release = resolve;
        }));
        const view = render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const page = await screen.findByTestId("companion-pdf-page-1");
        selectHello(page);
        fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });
        fireEvent.mouseDown(screen.getByTestId("pdf-translate"));
        expect(translate).toHaveBeenCalledTimes(1);
        view.unmount();

        render(<CompanionPdfReader absPath="c:/docs/paper.pdf" theme={theme} lang="zh-Hans" />);
        const again = await screen.findByTestId("companion-pdf-page-1");
        box(again, 200, 400);
        expect(await screen.findByRole("button", { name: "正在翻译" })).toBeTruthy();
        fireEvent.contextMenu(again, { clientX: 20, clientY: 30 });
        expect(screen.queryByTestId("pdf-translate-clear")).toBeNull();
        fireEvent.mouseDown(screen.getByTestId("pdf-translate"));
        expect(translate).toHaveBeenCalledTimes(1);

        await act(async () => {
            savePdfTranslations("C:/docs/Paper.PDF", [{
                id: "kept",
                text: "Keep",
                translation: "保留",
                parts: [{ page: 1, rects: [{ x: 0.2, y: 0.2, w: 0.2, h: 0.05 }] }],
            }]);
        });
        await act(async () => {
            release("你好");
        });
        await waitFor(() => expect(loadPdfTranslations("C:/docs/Paper.PDF").some((item) => item.translation === "你好")).toBe(true));
        const stored = loadPdfTranslations("C:/docs/Paper.PDF");
        expect(stored.some((item) => item.text === "Hello")).toBe(true);
        expect(stored.some((item) => item.translation === "保留")).toBe(true);
        expect(stored.some((item) => item.text === "Keep")).toBe(true);
        expect(screen.getAllByRole("button", { name: "译文" })).toHaveLength(2);
    });

    it("clears a finished translation from the right-click menu", async () => {
        pdfWithPages(1);
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const page = await screen.findByTestId("companion-pdf-page-1");
        selectHello(page);
        fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });
        fireEvent.mouseDown(screen.getByTestId("pdf-translate"));
        const icon = await screen.findByRole("button", { name: "译文" });
        const marks = JSON.stringify(loadPdfMarks("C:/docs/Paper.PDF"));
        fireEvent.mouseDown(icon, { button: 2 });
        expect(screen.queryByRole("dialog")).toBeNull();
        fireEvent.mouseDown(icon);
        const pop = screen.getByRole("dialog");
        expect(pop.textContent).toContain("你好");
        expect(screen.getByTestId("companion-pdf-page-1").className).toContain("companion-pdf-page--note");
        const nativeMenu = createEvent.contextMenu(pop);
        fireEvent(pop, nativeMenu);
        expect(nativeMenu.defaultPrevented).toBe(false);
        expect(screen.queryByTestId("pdf-highlight-menu")).toBeNull();
        fireEvent.contextMenu(icon);
        fireEvent.mouseDown(screen.getByTestId("pdf-translate-clear"));
        await waitFor(() => expect(screen.queryByRole("button", { name: "译文" })).toBeNull());
        expect(loadPdfTranslations("C:/docs/Paper.PDF")).toHaveLength(0);
        expect(JSON.stringify(loadPdfMarks("C:/docs/Paper.PDF"))).toBe(marks);
    });

    it("explains a login or credit failure", async () => {
        pdfWithPages(1);
        translate.mockRejectedValue(new Error("MaClaw Hub login is required"));
        const first = render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const page = await screen.findByTestId("companion-pdf-page-1");
        selectHello(page);
        fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });
        fireEvent.mouseDown(screen.getByTestId("pdf-translate"));
        expect((await screen.findByTestId("pdf-translate-error")).textContent).toContain("需要先在主窗口登录");
        await waitFor(() => expect(document.querySelector(".companion-pdf-translation")).toBeNull());
        first.unmount();
        resetPdfTranslationFlight();

        translate.mockRejectedValue(new Error('HTTP 403 {"code":"LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST","message":"insufficient credits for this request: need 1 credits, available 0"}'));
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const again = await screen.findByTestId("companion-pdf-page-1");
        selectHello(again);
        fireEvent.contextMenu(again, { clientX: 20, clientY: 30 });
        fireEvent.mouseDown(screen.getByTestId("pdf-translate"));
        await waitFor(() => expect(screen.getByTestId("pdf-translate-error").textContent).toContain("本次请求额度不足"));
    });

    it("shows Khalkha on the selection menu, swatches, and translation errors", async () => {
        pdfWithPages(1);
        translate.mockRejectedValue(new Error("翻译失败，请稍后重试"));
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="mn" />);
        const page = await screen.findByTestId("companion-pdf-page-1");
        selectHello(page);
        fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });
        expect(screen.getByTestId("pdf-highlight-menu").getAttribute("aria-label")).toBe("Сонгосон текстийн үйлдэл");
        expect(screen.getByTestId("pdf-translate").textContent).toContain("Орчуулах");
        expect(screen.getByTestId("pdf-translate").textContent).toContain("Хятад");
        expect(screen.getByTestId("pdf-highlight-yellow").getAttribute("aria-label")).toBe("Шар тодотгол");
        fireEvent.mouseDown(screen.getByTestId("pdf-translate"));
        expect((await screen.findByTestId("pdf-translate-error")).textContent).toContain("Орчуулга амжилтгүй. Дараа дахин оролдоно уу.");
        fireEvent.click(screen.getByTestId("pdf-draw-toggle"));
        expect(screen.getByTestId("pdf-draw-color-yellow").getAttribute("aria-label")).toBe("Шар үзэг");
    });

    it("keeps the newest translations when the store is full", () => {
        const items = Array.from({ length: 201 }, (_, index) => ({
            id: `t${index}`,
            text: `s${index}`,
            translation: `c${index}`,
            parts: [{ page: 1, rects: [{ x: 0.1, y: 0.1, w: 0.2, h: 0.05 }] }],
        }));
        const saved = savePdfTranslations("C:/docs/cap.pdf", items);
        expect(saved).toHaveLength(200);
        expect(saved[0].id).toBe("t1");
        expect(saved[saved.length - 1].id).toBe("t200");
    });

    it("shows the selection, groups the actions, and copies the source text", async () => {
        pdfWithPages(1);
        const writeText = vi.fn(async () => undefined);
        vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } });
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const page = await screen.findByTestId("companion-pdf-page-1");
        selectHello(page);
        fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });

        const menu = screen.getByTestId("pdf-highlight-menu");
        expect(menu.getAttribute("aria-label")).toBe("选中文本操作");
        expect(screen.getByTestId("pdf-menu-quote").textContent).toBe("Hello");
        // The colour swatches are one row of four, each labelled for a screen reader.
        expect(screen.getByTestId("pdf-highlight-yellow").getAttribute("aria-label")).toBe("黄色高亮");
        expect(menu.querySelectorAll(".companion-pdf-menu-colors .companion-pdf-menu-color")).toHaveLength(4);

        fireEvent.mouseDown(screen.getByTestId("pdf-copy"));
        await waitFor(() => expect(writeText).toHaveBeenCalledWith("Hello"));
        expect(screen.queryByTestId("pdf-highlight-menu")).toBeNull();
        expect(loadPdfMarks("C:/docs/Paper.PDF")).toHaveLength(0);
    });

    it("moves through the menu with the arrow keys and closes on Escape", async () => {
        pdfWithPages(1);
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const page = await screen.findByTestId("companion-pdf-page-1");
        selectHello(page);
        fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });

        const menu = screen.getByTestId("pdf-highlight-menu");
        const items = Array.from(menu.querySelectorAll<HTMLElement>('[role="menuitem"]'));
        expect(items.length).toBeGreaterThan(3);
        expect(document.activeElement).toBe(items[0]);

        fireEvent.keyDown(menu, { key: "ArrowDown" });
        expect(document.activeElement).toBe(items[1]);
        fireEvent.keyDown(menu, { key: "End" });
        expect(document.activeElement).toBe(items[items.length - 1]);
        fireEvent.keyDown(menu, { key: "ArrowDown" });
        expect(document.activeElement).toBe(items[0]);

        fireEvent.keyDown(window, { key: "Escape" });
        await waitFor(() => expect(screen.queryByTestId("pdf-highlight-menu")).toBeNull());
    });

    it("keeps the menu inside the window when the selection sits at the edge", async () => {
        pdfWithPages(1);
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const page = await screen.findByTestId("companion-pdf-page-1");
        selectHello(page);
        const orig = HTMLElement.prototype.getBoundingClientRect;
        HTMLElement.prototype.getBoundingClientRect = function () {
            if (this.dataset.testid === "pdf-highlight-menu") return clientRect(0, 0, 208, 220);
            return orig.call(this);
        };
        try {
            fireEvent.contextMenu(page, { clientX: window.innerWidth - 4, clientY: window.innerHeight - 4 });
            const menu = screen.getByTestId("pdf-highlight-menu");
            const left = Number.parseFloat(menu.style.left);
            const top = Number.parseFloat(menu.style.top);
            expect(left).toBeLessThanOrEqual(window.innerWidth - 208 - 8);
            expect(top).toBeLessThanOrEqual(window.innerHeight - 220 - 8);
            expect(left).toBeGreaterThanOrEqual(8);
            expect(top).toBeGreaterThanOrEqual(8);
        } finally {
            HTMLElement.prototype.getBoundingClientRect = orig;
        }
    });

    it("labels the translation card and copies the Chinese from it", async () => {
        pdfWithPages(1);
        const writeText = vi.fn(async () => undefined);
        vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } });
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const page = await screen.findByTestId("companion-pdf-page-1");
        selectHello(page);
        fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });
        fireEvent.mouseDown(screen.getByTestId("pdf-translate"));
        const icon = await screen.findByRole("button", { name: "译文" });
        fireEvent.mouseDown(icon);

        const pop = screen.getByRole("dialog");
        expect(pop.getAttribute("aria-label")).toBe("译文");
        expect(pop.querySelector(".companion-pdf-translation-kicker")?.textContent).toBe("中文译文");
        expect(pop.querySelector(".companion-pdf-translation-source")?.textContent).toContain("Hello");
        const copy = pop.querySelector('[aria-label="复制译文"]') as HTMLElement;
        expect(copy).toBeTruthy();
        fireEvent.click(copy);
        await waitFor(() => expect(writeText).toHaveBeenCalledWith("你好"));
        await waitFor(() => expect(pop.querySelector('[aria-label="已复制"]')).toBeTruthy());

        fireEvent.click(pop.querySelector('[aria-label="关闭译文"]') as HTMLElement);
        await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    });

    it("keeps the menu open while its own content scrolls", async () => {
        pdfWithPages(1);
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const page = await screen.findByTestId("companion-pdf-page-1");
        selectHello(page);
        fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });
        const menu = screen.getByTestId("pdf-highlight-menu");
        // A scroll inside the menu must not be mistaken for the page moving.
        fireEvent.scroll(menu);
        expect(screen.getByTestId("pdf-highlight-menu")).toBeTruthy();
        // A scroll of the reader still dismisses it.
        fireEvent.scroll(screen.getByTestId("companion-pdf-scroll"));
        await waitFor(() => expect(screen.queryByTestId("pdf-highlight-menu")).toBeNull());
    });

    it("says so when copying the source text fails", async () => {
        pdfWithPages(1);
        const writeText = vi.fn(async () => { throw new Error("clipboard unavailable"); });
        vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } });
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const page = await screen.findByTestId("companion-pdf-page-1");
        selectHello(page);
        fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });
        fireEvent.mouseDown(screen.getByTestId("pdf-copy"));
        expect((await screen.findByTestId("pdf-translate-error")).textContent).toContain("复制失败");
    });

    it("keeps the whole selection in the quote tooltip", async () => {
        pdfWithPages(1);
        const long = "A".repeat(200);
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const page = await screen.findByTestId("companion-pdf-page-1");
        box(page, 200, 400);
        const rect = { left: 10, top: 20, right: 50, bottom: 32, width: 40, height: 12 };
        vi.spyOn(window, "getSelection").mockReturnValue({
            isCollapsed: false,
            rangeCount: 1,
            toString: () => long,
            removeAllRanges: () => undefined,
            getRangeAt: () => ({ getClientRects: () => [rect] }),
        } as unknown as Selection);
        fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });
        const quote = screen.getByTestId("pdf-menu-quote");
        expect(quote.textContent?.endsWith("…")).toBe(true);
        expect(quote.getAttribute("title")).toBe(long);
    });

    it("draws one dashed box per selected line when a line is many client rects", async () => {
        pdfWithPages(1, "abstract paragraph");
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const page = await screen.findByTestId("companion-pdf-page-1");
        box(page, 200, 400);
        const lines = 8;
        const fragments = 7;
        const rects: ReturnType<typeof clientRect>[] = [];
        for (let line = 0; line < lines; line += 1) {
            // Gap is 10px, wider than 0.75× the 12px run. Every gap matches, so
            // the line stays one box instead of being read as a column gutter.
            for (let piece = 0; piece < fragments; piece += 1) {
                rects.push(clientRect(10 + piece * 16, 10 + line * 18, 6, 12));
            }
        }
        vi.spyOn(window, "getSelection").mockReturnValue({
            isCollapsed: false,
            rangeCount: 1,
            toString: () => "abstract paragraph",
            removeAllRanges: () => undefined,
            getRangeAt: () => ({ getClientRects: () => rects }),
        } as unknown as Selection);
        fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });
        fireEvent.mouseDown(screen.getByTestId("pdf-translate"));
        await screen.findByRole("button", { name: "译文" });
        const drawn = document.querySelectorAll(".companion-pdf-translation");
        expect(drawn.length).toBe(lines);
        const tops = Array.from(drawn).map((node) => Number.parseFloat((node as HTMLElement).style.top));
        expect(Math.max(...tops)).toBeCloseTo((10 + (lines - 1) * 18) / 400 * 100, 5);
        expect(Number.parseFloat((drawn[0] as HTMLElement).style.width)).toBeCloseTo((6 + (fragments - 1) * 16) / 200 * 100, 5);
    });

    it("boxes every selected line when the range client rects stop early", async () => {
        const lines = 10;
        const getPage = vi.fn(async () => ({
            getViewport: ({ scale }: { scale: number }) => ({
                width: 200 * scale,
                height: 400 * scale,
                scale,
                transform: [scale, 0, 0, -scale, 0, 400 * scale],
            }),
            getTextContent: async () => ({
                items: Array.from({ length: lines }, (_, index) => ({
                    str: `line ${index} `,
                    transform: [12, 0, 0, 12, 40, 360 - index * 14],
                    width: 80,
                    height: 12,
                })),
            }),
            render: () => ({ promise: Promise.resolve(), cancel: () => undefined }),
        }));
        getDocument.mockImplementation(() => ({
            promise: Promise.resolve({ numPages: 1, getPage, destroy: () => undefined }),
            destroy: () => undefined,
        }));
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const page = await screen.findByTestId("companion-pdf-page-1");
        box(page, 200, 400);
        const spans = page.querySelectorAll<HTMLElement>(".companion-pdf-text span");
        expect(spans.length).toBe(lines);
        spans.forEach((span, index) => {
            span.getBoundingClientRect = () => clientRect(40, 20 + index * 16, 120, 12) as DOMRect;
        });
        vi.spyOn(window, "getSelection").mockReturnValue({
            isCollapsed: false,
            rangeCount: 1,
            toString: () => Array.from({ length: lines }, (_, index) => `line ${index}`).join(" "),
            removeAllRanges: () => undefined,
            getRangeAt: () => ({
                intersectsNode: () => true,
                getClientRects: () => [clientRect(40, 20, 120, 12), clientRect(40, 36, 120, 12)],
                startContainer: document.createTextNode(""),
                endContainer: document.createTextNode(""),
            }),
        } as unknown as Selection);
        fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });
        fireEvent.mouseDown(screen.getByTestId("pdf-translate"));
        await screen.findByRole("button", { name: "译文" });
        const drawn = document.querySelectorAll(".companion-pdf-translation");
        expect(drawn.length).toBe(lines);
        const tops = Array.from(drawn).map((node) => Number.parseFloat((node as HTMLElement).style.top));
        expect(Math.max(...tops)).toBeCloseTo((20 + (lines - 1) * 16) / 400 * 100, 5);
    });

    it("keeps a narrow gutter apart, drops a covering rect, and leaves the line under a formula", async () => {
        pdfWithPages(1, "two columns");
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const page = await screen.findByTestId("companion-pdf-page-1");
        box(page, 200, 400);
        const rects = [
            clientRect(10, 30, 50, 12),
            // 10px gutter: wider than a word space, with no neighbor gap to call it justification.
            clientRect(70, 28, 50, 12),
            clientRect(8, 20, 184, 40),
            clientRect(10, 90, 22, 12),
            clientRect(34, 78, 24, 28),
            clientRect(74, 90, 40, 12),
            clientRect(10, 110, 50, 12),
            clientRect(10, 200, 20, 12),
            clientRect(36, 200, 20, 12),
        ];
        vi.spyOn(window, "getSelection").mockReturnValue({
            isCollapsed: false,
            rangeCount: 1,
            toString: () => "two columns",
            removeAllRanges: () => undefined,
            getRangeAt: () => ({ getClientRects: () => rects }),
        } as unknown as Selection);
        fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });
        fireEvent.mouseDown(screen.getByTestId("pdf-translate"));
        await screen.findByRole("button", { name: "译文" });
        const drawn = Array.from(document.querySelectorAll<HTMLElement>(".companion-pdf-translation"));
        expect(drawn).toHaveLength(6);
        const widths = drawn.map((node) => Number.parseFloat(node.style.width));
        expect(Math.max(...widths)).toBeLessThan(40);
        const heights = drawn.map((node) => Number.parseFloat(node.style.height));
        expect(heights.some((height) => height > 5)).toBe(true);
        const tops = drawn.map((node) => Number.parseFloat(node.style.top));
        expect(tops.some((top) => Math.abs(top - (78 / 400) * 100) < 0.05)).toBe(true);
        expect(tops.some((top) => Math.abs(top - (90 / 400) * 100) < 0.05)).toBe(true);
        expect(tops.some((top) => Math.abs(top - (110 / 400) * 100) < 0.05)).toBe(true);
        expect(tops.some((top) => Math.abs(top - (200 / 400) * 100) < 0.05)).toBe(true);
    });

    it("treats a wide space run as the gutter and keeps equal gaps on one line", async () => {
        const pieces = ["Left", " ", "Right", "A", " ", "B", " ", "C", " ", "D"];
        const getPage = vi.fn(async () => ({
            getViewport: ({ scale }: { scale: number }) => ({
                width: 200 * scale,
                height: 400 * scale,
                scale,
                transform: [scale, 0, 0, -scale, 0, 400 * scale],
            }),
            getTextContent: async () => ({
                items: pieces.map((str) => ({
                    str,
                    transform: [12, 0, 0, 12, 40, 360],
                    width: 40,
                    height: 12,
                })),
            }),
            render: () => ({ promise: Promise.resolve(), cancel: () => undefined }),
        }));
        getDocument.mockImplementation(() => ({
            promise: Promise.resolve({ numPages: 1, getPage, destroy: () => undefined }),
            destroy: () => undefined,
        }));
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const page = await screen.findByTestId("companion-pdf-page-1");
        box(page, 200, 400);
        const spans = page.querySelectorAll<HTMLElement>(".companion-pdf-text span");
        expect(spans.length).toBe(pieces.length);
        const rects = [
            clientRect(10, 30, 40, 12),
            clientRect(50, 30, 24, 12),
            clientRect(74, 30, 40, 12),
            clientRect(10, 60, 8, 12),
            clientRect(18, 60, 10, 12),
            clientRect(28, 60, 8, 12),
            clientRect(36, 60, 10, 12),
            clientRect(46, 60, 8, 12),
            clientRect(54, 60, 10, 12),
            clientRect(64, 60, 8, 12),
        ];
        spans.forEach((span, index) => {
            span.getBoundingClientRect = () => rects[index] as DOMRect;
        });
        vi.spyOn(window, "getSelection").mockReturnValue({
            isCollapsed: false,
            rangeCount: 1,
            toString: () => "Left Right A B C D",
            removeAllRanges: () => undefined,
            getRangeAt: () => ({
                intersectsNode: () => true,
                getClientRects: () => [clientRect(10, 30, 104, 42)],
                startContainer: document.createTextNode(""),
                endContainer: document.createTextNode(""),
            }),
        } as unknown as Selection);
        fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });
        fireEvent.mouseDown(screen.getByTestId("pdf-translate"));
        await screen.findByRole("button", { name: "译文" });
        const drawn = Array.from(document.querySelectorAll<HTMLElement>(".companion-pdf-translation"));
        expect(drawn).toHaveLength(3);
        const widths = drawn.map((node) => Number.parseFloat(node.style.width));
        expect(widths.filter((width) => Math.abs(width - (40 / 200) * 100) < 0.05)).toHaveLength(2);
        expect(widths.some((width) => Math.abs(width - (62 / 200) * 100) < 0.05)).toBe(true);
    });

    it("clips the first run from the text offset instead of the range rects", async () => {
        pdfWithPages(1, "Hello world");
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const page = await screen.findByTestId("companion-pdf-page-1");
        box(page, 200, 400);
        const span = page.querySelector<HTMLElement>(".companion-pdf-text span");
        if (!span?.firstChild) throw new Error("missing text run");
        span.getBoundingClientRect = () => clientRect(40, 20, 110, 12) as DOMRect;
        const text = span.firstChild;
        const length = text.textContent?.length ?? 0;
        vi.spyOn(window, "getSelection").mockReturnValue({
            isCollapsed: false,
            rangeCount: 1,
            toString: () => "world",
            removeAllRanges: () => undefined,
            getRangeAt: () => ({
                intersectsNode: (node: Node) => node === span,
                startContainer: text,
                startOffset: length - 5,
                endContainer: text,
                endOffset: length,
                getClientRects: () => [clientRect(0, 0, 200, 400)],
            }),
        } as unknown as Selection);
        fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });
        fireEvent.mouseDown(screen.getByTestId("pdf-translate"));
        await screen.findByRole("button", { name: "译文" });
        const drawn = document.querySelector(".companion-pdf-translation") as HTMLElement;
        expect(Number.parseFloat(drawn.style.width)).toBeCloseTo((110 * 5) / length / 200 * 100, 5);
        expect(Number.parseFloat(drawn.style.left)).toBeCloseTo((40 + 110 * (length - 5) / length) / 200 * 100, 5);
        expect(Number.parseFloat(drawn.style.height)).toBeCloseTo(12 / 400 * 100, 5);
    });

    it("keeps the bookmark list between its minimum and the paper", () => {
        expect(clampPdfBookmarkRailWidth(10, 800)).toBe(96);
        expect(clampPdfBookmarkRailWidth(900, 800)).toBe(480);
        expect(clampPdfBookmarkRailWidth(400, 500)).toBe(260);
        expect(clampPdfBookmarkRailWidth(200, 0)).toBe(200);
        expect(clampPdfBookmarkRailWidth(Number.NaN, 800)).toBe(156);
        expect(nextPdfBookmarkRailPreferred(400, 300, 500)).toBe(400);
        expect(nextPdfBookmarkRailPreferred(400, 260, 500)).toBe(400);
        expect(nextPdfBookmarkRailPreferred(400, 259.6, 500)).toBe(400);
        expect(nextPdfBookmarkRailPreferred(400, 259.4, 500)).toBe(259);
        expect(nextPdfBookmarkRailPreferred(400, 200, 500)).toBe(200);
        expect(nextPdfBookmarkRailPreferred(200, 300, 500)).toBe(260);
        expect(nextPdfBookmarkRailPreferred(200, 80, 100)).toBe(200);
        expect(nextPdfBookmarkRailPreferred(156, 236, 800)).toBe(236);
        expect(nextPdfBookmarkRailPreferred(400, Number.NaN, 500)).toBe(400);
    });

    it("drags the left bookmark list and remembers the width", async () => {
        pdfWithPages(1, "OnlyOne");
        hydratePdfBookmarks({
            "c:/docs/paper.pdf": [
                { id: "a", page: 1, x: 0.2, y: 0.4, color: "yellow", name: "甲" },
            ],
        });
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const handle = await screen.findByTestId("pdf-bookmark-resize");
        const column = screen.getByTestId("pdf-bookmark-column");
        expect(column.style.width).toBe("156px");
        expect(handle.getAttribute("aria-label")).toBe("拖动调整书签列表宽度");
        const stage = column.parentElement as HTMLElement;
        Object.defineProperty(stage, "clientWidth", { configurable: true, value: 800 });

        fireEvent.pointerDown(handle, { button: 0, pointerId: 1, clientX: 100 });
        fireEvent.pointerMove(window, { pointerId: 1, clientX: 180 });
        expect(column.style.width).toBe("236px");
        fireEvent.pointerUp(window, { pointerId: 1, clientX: 180 });
        expect(localStorage.getItem(PDF_BOOKMARK_RAIL_STORAGE_KEY)).toBe("236");

        fireEvent.doubleClick(handle);
        expect(column.style.width).toBe("156px");
        expect(localStorage.getItem(PDF_BOOKMARK_RAIL_STORAGE_KEY)).toBe("156");

        fireEvent.keyDown(handle, { key: "ArrowRight" });
        expect(column.style.width).toBe("172px");
        fireEvent.keyDown(handle, { key: "Home" });
        expect(column.style.width).toBe("96px");
        expect(column.style.flexBasis).toBe("96px");
    });

    it("keeps a wider bookmark list while the window is already limiting it", async () => {
        const observers: Array<() => void> = [];
        class RecordingResizeObserver {
            constructor(callback: ResizeObserverCallback) {
                observers.push(() => callback([], this as unknown as ResizeObserver));
            }
            observe() {}
            unobserve() {}
            disconnect() {}
        }
        vi.stubGlobal("ResizeObserver", RecordingResizeObserver);
        localStorage.setItem(PDF_BOOKMARK_RAIL_STORAGE_KEY, "400");
        pdfWithPages(1, "OnlyOne");
        hydratePdfBookmarks({
            "c:/docs/paper.pdf": [
                { id: "a", page: 1, x: 0.2, y: 0.4, color: "yellow", name: "甲" },
            ],
        });
        const view = render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        try {
            const handle = await screen.findByTestId("pdf-bookmark-resize");
            const column = screen.getByTestId("pdf-bookmark-column");
            expect(column.style.width).toBe("400px");
            const stage = column.parentElement as HTMLElement;
            Object.defineProperty(stage, "clientWidth", { configurable: true, value: 500 });
            act(() => { observers.forEach((notify) => notify()); });
            expect(column.style.width).toBe("260px");
            expect(handle.getAttribute("aria-valuemax")).toBe("260");

            fireEvent.pointerDown(handle, { button: 0, pointerId: 1, clientX: 100 });
            fireEvent.pointerMove(window, { pointerId: 1, clientX: 100.4 });
            fireEvent.pointerUp(window, { pointerId: 1, clientX: 100.4 });
            expect(column.style.width).toBe("260px");
            expect(localStorage.getItem(PDF_BOOKMARK_RAIL_STORAGE_KEY)).toBe("400");

            fireEvent.keyDown(handle, { key: "ArrowRight" });
            expect(column.style.width).toBe("260px");
            expect(localStorage.getItem(PDF_BOOKMARK_RAIL_STORAGE_KEY)).toBe("400");

            Object.defineProperty(stage, "clientWidth", { configurable: true, value: 120 });
            act(() => { observers.forEach((notify) => notify()); });
            expect(column.style.width).toBe("96px");
            fireEvent.keyDown(handle, { key: "ArrowLeft" });
            expect(column.style.width).toBe("96px");
            expect(localStorage.getItem(PDF_BOOKMARK_RAIL_STORAGE_KEY)).toBe("400");

            Object.defineProperty(stage, "clientWidth", { configurable: true, value: 500 });
            act(() => { observers.forEach((notify) => notify()); });
            expect(column.style.width).toBe("260px");
            fireEvent.pointerDown(handle, { button: 0, pointerId: 2, clientX: 100 });
            fireEvent.pointerMove(window, { pointerId: 2, clientX: 80 });
            fireEvent.pointerUp(window, { pointerId: 2, clientX: 80 });
            expect(column.style.width).toBe("240px");
            expect(localStorage.getItem(PDF_BOOKMARK_RAIL_STORAGE_KEY)).toBe("240");
        } finally {
            view.unmount();
            vi.unstubAllGlobals();
        }
    });

    it("restores the pointer when the bookmark list unmounts during a drag", async () => {
        pdfWithPages(1, "OnlyOne");
        hydratePdfBookmarks({
            "c:/docs/paper.pdf": [
                { id: "a", page: 1, x: 0.2, y: 0.4, color: "yellow", name: "甲" },
            ],
        });
        const view = render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const handle = await screen.findByTestId("pdf-bookmark-resize");
        fireEvent.pointerDown(handle, { button: 0, pointerId: 1, clientX: 40 });
        expect(document.body.style.cursor).toBe("col-resize");
        view.unmount();
        expect(document.body.style.cursor).toBe("");
        expect(document.body.style.userSelect).toBe("");
        fireEvent.pointerMove(window, { pointerId: 1, clientX: 90 });
        fireEvent.pointerUp(window, { pointerId: 1, clientX: 90 });
        expect(localStorage.getItem(PDF_BOOKMARK_RAIL_STORAGE_KEY)).toBe("156");
    });

    it("clears the drag highlight after a resize under StrictMode", async () => {
        pdfWithPages(1, "OnlyOne");
        hydratePdfBookmarks({
            "c:/docs/paper.pdf": [
                { id: "a", page: 1, x: 0.2, y: 0.4, color: "yellow", name: "甲" },
            ],
        });
        render(
            <StrictMode>
                <CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />
            </StrictMode>,
        );
        const handle = await screen.findByTestId("pdf-bookmark-resize");
        const column = screen.getByTestId("pdf-bookmark-column");
        const stage = column.parentElement as HTMLElement;
        Object.defineProperty(stage, "clientWidth", { configurable: true, value: 800 });
        fireEvent.pointerDown(handle, { button: 0, pointerId: 1, clientX: 100 });
        expect(handle.getAttribute("data-dragging")).toBe("true");
        fireEvent.pointerMove(window, { pointerId: 1, clientX: 140 });
        fireEvent.pointerUp(window, { pointerId: 1, clientX: 140 });
        expect(handle.getAttribute("data-dragging")).toBe("false");
        expect(column.style.width).toBe("196px");
    });

    it("adds bookmarks in click order, jumps to the anchor, and renames or deletes from the bookmark menu", async () => {
        pdfWithPages(2, "OnlyOne");
        const announced = vi.fn();
        window.addEventListener(PDF_BOOKMARKS_CHANGED_EVENT, announced);
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        await screen.findByText("OnlyOne");
        const scroller = screen.getByTestId("companion-pdf-scroll");
        const page1 = screen.getByTestId("companion-pdf-page-1");
        const page2 = screen.getByTestId("companion-pdf-page-2");
        scroller.getBoundingClientRect = () => clientRect(0, 0, 300, 400);
        page1.getBoundingClientRect = () => clientRect(40, 0, 200, 400) as DOMRect;
        page2.getBoundingClientRect = () => clientRect(40, 900, 200, 400) as DOMRect;
        vi.spyOn(window, "getSelection").mockReturnValue({
            isCollapsed: true,
            rangeCount: 0,
            toString: () => "",
            removeAllRanges: () => undefined,
        } as unknown as Selection);

        const drawMenu = createEvent.contextMenu(screen.getByTestId("pdf-draw"));
        fireEvent(screen.getByTestId("pdf-draw"), drawMenu);
        expect(screen.queryByTestId("pdf-highlight-menu")).toBeNull();

        fireEvent.contextMenu(page1, { clientX: 140, clientY: 300 });
        const menu = screen.getByTestId("pdf-highlight-menu");
        expect(menu.textContent).toContain("添加书签");
        expect(screen.queryByTestId("pdf-highlight-yellow")).toBeNull();
        fireEvent.mouseDown(screen.getByTestId("pdf-bookmark-add"));
        const name = screen.getByTestId("pdf-bookmark-name") as HTMLInputElement;
        expect((screen.getByTestId("pdf-bookmark-save") as HTMLButtonElement).disabled).toBe(true);
        fireEvent.keyDown(name, { key: "Escape" });
        expect(screen.queryByTestId("pdf-bookmark-name")).toBeNull();
        expect(screen.getByTestId("pdf-bookmark-add")).toBeTruthy();

        fireEvent.mouseDown(screen.getByTestId("pdf-bookmark-add"));
        expect(screen.getByTestId("pdf-bookmark-color-yellow").getAttribute("aria-pressed")).toBe("true");
        fireEvent.click(screen.getByTestId("pdf-bookmark-color-orange"));
        expect(screen.getByTestId("pdf-bookmark-color-orange").getAttribute("aria-pressed")).toBe("true");
        expect(screen.getAllByTestId(/pdf-bookmark-color-/)).toHaveLength(8);
        fireEvent.change(screen.getByTestId("pdf-bookmark-name"), { target: { value: "先加" } });
        fireEvent.click(screen.getByTestId("pdf-bookmark-save"));

        fireEvent.contextMenu(page2, { clientX: 80, clientY: 940 });
        fireEvent.mouseDown(screen.getByTestId("pdf-bookmark-add"));
        fireEvent.click(screen.getByTestId("pdf-bookmark-color-teal"));
        fireEvent.change(screen.getByTestId("pdf-bookmark-name"), { target: { value: "后加" } });
        fireEvent.keyDown(screen.getByTestId("pdf-bookmark-name"), { key: "Enter" });

        const rail = screen.getByTestId("pdf-bookmark-rail");
        const names = () => Array.from(rail.querySelectorAll(".companion-pdf-bookmark-name")).map((node) => node.textContent);
        expect(names()).toEqual(["先加", "后加"]);
        const chips = () => Array.from(rail.querySelectorAll<HTMLElement>(".companion-pdf-bookmark"));
        expect(chips()[0].querySelector(".companion-pdf-bookmark-page")?.textContent).toBe("1");
        expect(chips()[1].querySelector(".companion-pdf-bookmark-page")?.textContent).toBe("2");
        expect(document.querySelector(".companion-pdf-stage")?.classList.contains("companion-pdf-stage--bookmarks")).toBe(true);
        expect(chips()[0].querySelector(".companion-pdf-bookmark-swatch path")?.getAttribute("fill")).toBe("#ffc078");
        expect(screen.queryByTestId("pdf-bookmark-tick-" + (chips()[1].dataset.pdfBookmark ?? ""))).toBeNull();

        await act(async () => {
            fireEvent.click(chips()[1]);
            await new Promise((resolve) => requestAnimationFrame(resolve));
            await new Promise((resolve) => requestAnimationFrame(resolve));
        });
        expect(scroller.scrollTop).toBe(868);
        const secondId = chips()[1].getAttribute("data-pdf-bookmark") ?? "";
        expect(screen.getByTestId(`pdf-bookmark-tick-${secondId}`)).toBeTruthy();

        fireEvent.contextMenu(chips()[0]);
        expect(screen.getByTestId("pdf-highlight-menu").getAttribute("aria-label")).toBe("书签操作");
        expect(screen.queryByTestId("pdf-bookmark-add")).toBeNull();
        fireEvent.mouseDown(screen.getByTestId("pdf-bookmark-rename"));
        expect(screen.getByTestId("pdf-bookmark-color-orange").getAttribute("aria-pressed")).toBe("true");
        fireEvent.click(screen.getByTestId("pdf-bookmark-color-blue"));
        fireEvent.change(screen.getByTestId("pdf-bookmark-name"), { target: { value: "改过" } });
        fireEvent.click(screen.getByTestId("pdf-bookmark-save"));
        expect(names()).toEqual(["改过", "后加"]);
        const stored = loadPdfBookmarks("C:/docs/Paper.PDF");
        expect(stored.map((item) => item.name)).toEqual(["改过", "后加"]);
        expect(stored[0].color).toBe("blue");
        expect(stored[0].page).toBe(1);
        expect(stored[0].y).toBeCloseTo(0.75);
        expect(stored[1].color).toBe("teal");
        expect(stored[1].page).toBe(2);
        expect(stored[1].y).toBeCloseTo(0.1);

        fireEvent.contextMenu(screen.getByTestId(`pdf-bookmark-tick-${secondId}`));
        fireEvent.mouseDown(screen.getByTestId("pdf-bookmark-delete"));
        expect(names()).toEqual(["改过"]);
        expect(loadPdfBookmarks("C:/docs/Paper.PDF")).toHaveLength(1);
        expect(announced).toHaveBeenCalled();
        window.removeEventListener(PDF_BOOKMARKS_CHANGED_EVENT, announced);
    });

    it("labels bookmark actions in Khalkha", async () => {
        pdfWithPages(1);
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="mn" />);
        const page = await screen.findByTestId("companion-pdf-page-1");
        page.getBoundingClientRect = () => clientRect(0, 0, 200, 400) as DOMRect;
        vi.spyOn(window, "getSelection").mockReturnValue({
            isCollapsed: true,
            rangeCount: 0,
            toString: () => "",
            removeAllRanges: () => undefined,
        } as unknown as Selection);
        fireEvent.contextMenu(page, { clientX: 20, clientY: 40 });
        expect(screen.getByTestId("pdf-bookmark-add").textContent).toContain("Хавчуурга нэмэх");
        fireEvent.mouseDown(screen.getByTestId("pdf-bookmark-add"));
        expect(screen.getByTestId("pdf-bookmark-name").getAttribute("placeholder")).toBe("Хавчуургын нэр");
        expect(screen.getByTestId("pdf-bookmark-color-orange").getAttribute("aria-label")).toBe("Улбар шар хавчуурга");
        expect(screen.getByTestId("pdf-bookmark-save").textContent).toBe("Баталгаажуулах");
    });

    it("names a new bookmark from the selection", async () => {
        pdfWithPages(1);
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const page = await screen.findByTestId("companion-pdf-page-1");
        selectHello(page);
        fireEvent.contextMenu(page, { clientX: 20, clientY: 30 });
        fireEvent.mouseDown(screen.getByTestId("pdf-bookmark-add"));
        expect((screen.getByTestId("pdf-bookmark-name") as HTMLInputElement).value).toBe("Hello");
        expect((screen.getByTestId("pdf-bookmark-save") as HTMLButtonElement).disabled).toBe(false);
        fireEvent.keyDown(screen.getByTestId("pdf-bookmark-name"), { key: "Enter", isComposing: true });
        expect(screen.getByTestId("pdf-bookmark-name")).toBeTruthy();
        expect(loadPdfBookmarks("C:/docs/Paper.PDF")).toEqual([]);
        fireEvent.keyDown(screen.getByTestId("pdf-bookmark-name"), { key: "Enter" });
        expect(loadPdfBookmarks("C:/docs/Paper.PDF").map((item) => item.name)).toEqual(["Hello"]);
    });

    it("keeps nearby bookmark ticks side by side", async () => {
        pdfWithPages(1, "OnlyOne");
        hydratePdfBookmarks({
            "c:/docs/paper.pdf": [
                { id: "a", page: 1, x: 0.2, y: 0.5, color: "yellow", name: "甲" },
                { id: "b", page: 1, x: 0.2, y: 0.51, color: "teal", name: "乙" },
            ],
        });
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        const first = await screen.findByTestId("pdf-bookmark-tick-a");
        const second = screen.getByTestId("pdf-bookmark-tick-b");
        expect(first.style.left).toBe("0px");
        expect(first.style.top).toBe("200px");
        expect(first.style.zIndex).toBe("");
        expect(first.tabIndex).toBe(-1);
        expect(second.style.left).toBe("18px");
        expect(second.style.zIndex).toBe("");
        expect(first.querySelector("path")?.getAttribute("fill")).toBe("#ffe08a");
        expect(second.querySelector("path")?.getAttribute("fill")).toBe("#9ad8d0");
        expect(screen.getByTestId("pdf-bookmark-a").classList.contains("companion-pdf-bookmark--here")).toBe(true);
        expect(screen.getByTestId("pdf-bookmark-b").classList.contains("companion-pdf-bookmark--here")).toBe(true);
        expect(screen.getByTestId("pdf-bookmark-a").getAttribute("aria-label")).toBe("第 1 页，甲");
        fireEvent.click(screen.getByTestId("pdf-bookmark-b"));
        expect(screen.getByTestId("pdf-bookmark-b").classList.contains("companion-pdf-bookmark--here")).toBe(true);
        expect(screen.getByTestId("pdf-bookmark-a").classList.contains("companion-pdf-bookmark--here")).toBe(false);
    });

    it("follows a bookmark after the page grows out of the placeholder size", async () => {
        const callbacks: Array<() => void> = [];
        class RecordingObserver {
            constructor(cb: () => void) {
                callbacks.push(cb);
            }
            observe() {}
            unobserve() {}
            disconnect() {}
        }
        vi.stubGlobal("ResizeObserver", RecordingObserver);
        try {
            pdfWithPages(2, "OnlyOne");
            hydratePdfBookmarks({
                "c:/docs/paper.pdf": [{ id: "late", page: 2, x: 0.2, y: 0.1, color: "teal", name: "后加" }],
            });
            render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
            await screen.findByText("OnlyOne");
            const scroller = screen.getByTestId("companion-pdf-scroll");
            const page2 = screen.getByTestId("companion-pdf-page-2");
            scroller.getBoundingClientRect = () => clientRect(0, 0, 300, 400);
            let pageHeight = 400;
            let pageDocTop = 900;
            page2.getBoundingClientRect = () => clientRect(40, pageDocTop - scroller.scrollTop, 200, pageHeight) as DOMRect;
            fireEvent.click(screen.getByTestId("pdf-bookmark-late"));
            await act(async () => {
                await new Promise((resolve) => requestAnimationFrame(resolve));
                await new Promise((resolve) => requestAnimationFrame(resolve));
            });
            expect(scroller.scrollTop).toBe(868);
            const follow = callbacks[callbacks.length - 1];
            expect(typeof follow).toBe("function");
            // A shell above this page grows. The mark's own height is unchanged.
            pageDocTop = 1100;
            act(() => follow());
            expect(scroller.scrollTop).toBe(1068);
            pageHeight = 800;
            act(() => follow());
            expect(scroller.scrollTop).toBe(1108);
            // The reader scrolled. A shell that grows afterwards stays where they left it.
            scroller.scrollTop = 200;
            fireEvent.scroll(scroller);
            pageDocTop = 1400;
            act(() => follow());
            expect(scroller.scrollTop).toBe(200);

            // The page is ready, so the resize watch ends and a later shell stays put.
            // The scroll listener is still there until the reader moves.
            pageDocTop = 900;
            pageHeight = 400;
            await act(async () => {
                fireEvent.click(screen.getByTestId("pdf-bookmark-late"));
                await new Promise((resolve) => requestAnimationFrame(resolve));
                await new Promise((resolve) => requestAnimationFrame(resolve));
            });
            expect(scroller.scrollTop).toBe(868);
            const settled = callbacks[callbacks.length - 1];
            await waitFor(() => expect(page2.hasAttribute("data-pdf-ready")).toBe(true));
            await act(async () => {
                await new Promise((resolve) => setTimeout(resolve, 450));
            });
            pageDocTop = 1400;
            act(() => settled());
            expect(scroller.scrollTop).toBe(868);

            // Deleting the mark stops that jump. A shell that grows afterwards stays.
            pageDocTop = 900;
            pageHeight = 400;
            await act(async () => {
                fireEvent.click(screen.getByTestId("pdf-bookmark-late"));
                await new Promise((resolve) => requestAnimationFrame(resolve));
                await new Promise((resolve) => requestAnimationFrame(resolve));
            });
            expect(scroller.scrollTop).toBe(868);
            const live = callbacks[callbacks.length - 1];
            fireEvent.contextMenu(screen.getByTestId("pdf-bookmark-late"));
            fireEvent.mouseDown(screen.getByTestId("pdf-bookmark-delete"));
            expect(screen.queryByTestId("pdf-bookmark-rail")).toBeNull();
            pageDocTop = 1400;
            act(() => live());
            expect(scroller.scrollTop).toBe(868);
        } finally {
            vi.unstubAllGlobals();
        }
    });

    it("keeps the bookmark in place while a page above it is still loading", async () => {
        const callbacks: Array<() => void> = [];
        class RecordingObserver {
            constructor(cb: () => void) {
                callbacks.push(cb);
            }
            observe() {}
            unobserve() {}
            disconnect() {}
        }
        vi.stubGlobal("ResizeObserver", RecordingObserver);
        let releasePage1 = () => {};
        let released = false;
        const gate = new Promise<void>((resolve) => {
            releasePage1 = () => {
                if (released) return;
                released = true;
                resolve();
            };
        });
        const size = { width: 200, height: 400 };
        getDocument.mockImplementation(() => ({
            promise: Promise.resolve({
                numPages: 2,
                getPage: vi.fn(async (page: number) => {
                    if (page === 1) await gate;
                    return {
                        getViewport: ({ scale }: { scale: number }) => ({
                            width: size.width * scale,
                            height: size.height * scale,
                            scale,
                            transform: [scale, 0, 0, -scale, 0, size.height * scale],
                        }),
                        getTextContent: async () => ({ items: [] }),
                        render: () => ({ promise: Promise.resolve(), cancel: () => undefined }),
                    };
                }),
                destroy: () => undefined,
            }),
            destroy: () => undefined,
        }));
        try {
            hydratePdfBookmarks({
                "c:/docs/paper.pdf": [{ id: "late", page: 2, x: 0.2, y: 0.1, color: "teal", name: "后加" }],
            });
            render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
            await screen.findByTestId("pdf-bookmark-late");
            const scroller = screen.getByTestId("companion-pdf-scroll");
            const page1 = screen.getByTestId("companion-pdf-page-1");
            const page2 = screen.getByTestId("companion-pdf-page-2");
            expect(page1.querySelector("canvas")).toBeTruthy();
            expect(page1.hasAttribute("data-pdf-ready")).toBe(false);
            scroller.getBoundingClientRect = () => clientRect(0, 0, 300, 400);
            let pageHeight = 400;
            let pageDocTop = 900;
            page2.getBoundingClientRect = () => clientRect(40, pageDocTop - scroller.scrollTop, 200, pageHeight) as DOMRect;
            await act(async () => {
                fireEvent.click(screen.getByTestId("pdf-bookmark-late"));
                await new Promise((resolve) => requestAnimationFrame(resolve));
                await new Promise((resolve) => requestAnimationFrame(resolve));
            });
            expect(scroller.scrollTop).toBe(868);
            const follow = callbacks[callbacks.length - 1];
            await act(async () => {
                await new Promise((resolve) => setTimeout(resolve, 450));
            });
            pageDocTop = 1100;
            act(() => follow());
            expect(scroller.scrollTop).toBe(1068);
            releasePage1();
            await waitFor(() => expect(page1.hasAttribute("data-pdf-ready")).toBe(true));
            await waitFor(() => expect(page2.hasAttribute("data-pdf-ready")).toBe(true));
            await act(async () => {
                await new Promise((resolve) => setTimeout(resolve, 450));
            });
            pageDocTop = 1400;
            act(() => follow());
            expect(scroller.scrollTop).toBe(1068);
        } finally {
            releasePage1();
            vi.unstubAllGlobals();
        }
    });

    it("keeps the jumped bookmark highlighted while the previous page peeks, then follows a scroll", async () => {
        pdfWithPages(2, "OnlyOne");
        hydratePdfBookmarks({
            "c:/docs/paper.pdf": [
                { id: "a", page: 1, x: 0.2, y: 0.5, color: "yellow", name: "甲" },
                { id: "b", page: 2, x: 0.2, y: 0.05, color: "teal", name: "乙" },
            ],
        });
        render(<CompanionPdfReader absPath="C:/docs/Paper.PDF" theme={theme} lang="zh-Hans" />);
        await screen.findByText("OnlyOne");
        const scroller = screen.getByTestId("companion-pdf-scroll");
        Object.defineProperty(scroller, "clientHeight", { configurable: true, get: () => 400 });
        scroller.getBoundingClientRect = () => clientRect(0, 0, 300, 400);
        screen.getByTestId("companion-pdf-page-1").getBoundingClientRect = () => clientRect(0, -scroller.scrollTop, 200, 400) as DOMRect;
        screen.getByTestId("companion-pdf-page-2").getBoundingClientRect = () => clientRect(0, 400 - scroller.scrollTop, 200, 400) as DOMRect;
        expect(screen.getByTestId("pdf-bookmark-a").classList.contains("companion-pdf-bookmark--here")).toBe(true);

        await act(async () => {
            fireEvent.click(screen.getByTestId("pdf-bookmark-b"));
            await new Promise((resolve) => requestAnimationFrame(resolve));
            await new Promise((resolve) => requestAnimationFrame(resolve));
        });
        // The mark sits 72px down, so page 1 still owns the top edge.
        expect(scroller.scrollTop).toBe(348);
        await act(async () => {
            fireEvent.scroll(scroller);
            await new Promise((resolve) => requestAnimationFrame(resolve));
        });
        expect(screen.getByTestId("pdf-bookmark-b").classList.contains("companion-pdf-bookmark--here")).toBe(true);
        expect(screen.getByTestId("pdf-bookmark-a").classList.contains("companion-pdf-bookmark--here")).toBe(false);

        scroller.scrollTop = 0;
        await act(async () => {
            fireEvent.scroll(scroller);
            await new Promise((resolve) => requestAnimationFrame(resolve));
        });
        expect(screen.getByTestId("pdf-bookmark-a").classList.contains("companion-pdf-bookmark--here")).toBe(true);
        expect(screen.getByTestId("pdf-bookmark-b").classList.contains("companion-pdf-bookmark--here")).toBe(false);

        // After the page is ready the resize watch ends, and the list still follows a scroll.
        await act(async () => {
            fireEvent.click(screen.getByTestId("pdf-bookmark-b"));
            await new Promise((resolve) => requestAnimationFrame(resolve));
            await new Promise((resolve) => requestAnimationFrame(resolve));
        });
        await waitFor(() => expect(screen.getByTestId("companion-pdf-page-2").hasAttribute("data-pdf-ready")).toBe(true));
        await act(async () => {
            await new Promise((resolve) => setTimeout(resolve, 450));
        });
        scroller.scrollTop = 0;
        await act(async () => {
            fireEvent.scroll(scroller);
            await new Promise((resolve) => requestAnimationFrame(resolve));
        });
        expect(screen.getByTestId("pdf-bookmark-a").classList.contains("companion-pdf-bookmark--here")).toBe(true);
        expect(screen.getByTestId("pdf-bookmark-b").classList.contains("companion-pdf-bookmark--here")).toBe(false);

        await act(async () => {
            fireEvent.click(screen.getByTestId("pdf-bookmark-b"));
            await new Promise((resolve) => requestAnimationFrame(resolve));
            await new Promise((resolve) => requestAnimationFrame(resolve));
        });
        expect(scroller.scrollTop).toBe(348);
        fireEvent.contextMenu(screen.getByTestId("pdf-bookmark-b"));
        fireEvent.mouseDown(screen.getByTestId("pdf-bookmark-delete"));
        expect(screen.queryByTestId("pdf-bookmark-b")).toBeNull();
        expect(screen.getByTestId("pdf-bookmark-a").classList.contains("companion-pdf-bookmark--here")).toBe(true);
        expect(scroller.scrollTop).toBe(348);
    });
});
