import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CopyChatMediaToClipboard, SaveChatMediaAs } from "../../../../wailsjs/go/main/App";
import { MarkdownImagePreview } from "../markdownMedia";

vi.mock("../../../../wailsjs/go/main/App", () => ({
    CopyChatMediaToClipboard: vi.fn(async () => undefined),
    SaveChatMediaAs: vi.fn(async () => ""),
}));

const PNG = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==";

describe("chat image context menu", () => {
    afterEach(() => {
        vi.mocked(CopyChatMediaToClipboard).mockClear();
        vi.mocked(SaveChatMediaAs).mockClear();
        document.documentElement.lang = "";
    });

    it("copies and saves a markdown preview image from the right-click menu", async () => {
        document.documentElement.lang = "zh-Hans";
        render(<MarkdownImagePreview src={PNG} alt="roofline" />);
        fireEvent.contextMenu(screen.getByTestId("markdown-image-preview"));
        const menu = screen.getByTestId("chat-media-context-menu");
        expect(menu.textContent).toContain("复制");
        expect(menu.textContent).toContain("保存");
        expect(screen.queryByTestId("markdown-image-zoom")).toBeNull();

        fireEvent.click(screen.getByTestId("chat-media-save"));
        await waitFor(() => expect(SaveChatMediaAs).toHaveBeenCalledWith("image", PNG, "roofline", "保存"));

        fireEvent.contextMenu(screen.getByTestId("markdown-image-preview"));
        fireEvent.click(screen.getByTestId("chat-media-copy"));
        await waitFor(() => expect(CopyChatMediaToClipboard).toHaveBeenCalledWith("image", PNG));
    });

    it("closes the menu on Escape without opening the zoomed image", () => {
        render(<MarkdownImagePreview src={PNG} alt="roofline" />);
        fireEvent.contextMenu(screen.getByTestId("markdown-image-preview"));
        fireEvent.keyDown(window, { key: "Escape" });
        expect(screen.queryByTestId("chat-media-context-menu")).toBeNull();
        expect(screen.queryByTestId("markdown-image-zoom")).toBeNull();
    });

    it("Escape on a zoomed image closes the menu and leaves the zoom open", () => {
        render(<MarkdownImagePreview src={PNG} alt="roofline" />);
        fireEvent.click(screen.getByTestId("markdown-image-preview"));
        const zoom = screen.getByTestId("markdown-image-zoom");
        fireEvent.contextMenu(zoom.querySelector("img") as Element);
        expect(screen.getByTestId("chat-media-context-menu")).toBeTruthy();
        fireEvent.keyDown(window, { key: "Escape" });
        expect(screen.queryByTestId("chat-media-context-menu")).toBeNull();
        expect(screen.getByTestId("markdown-image-zoom")).toBeTruthy();
    });

    it("keeps a clamped menu inside the window while a copy is in progress", () => {
        vi.mocked(CopyChatMediaToClipboard).mockImplementation(() => new Promise(() => undefined));
        const rect = vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue({
            x: 0, y: 0, top: 0, left: 0, right: 200, bottom: 80, width: 200, height: 80, toJSON() { return {}; },
        });
        const width = Object.getOwnPropertyDescriptor(window, "innerWidth");
        const height = Object.getOwnPropertyDescriptor(window, "innerHeight");
        Object.defineProperty(window, "innerWidth", { configurable: true, value: 1024 });
        Object.defineProperty(window, "innerHeight", { configurable: true, value: 768 });
        try {
            render(<MarkdownImagePreview src={PNG} alt="roofline" />);
            fireEvent.contextMenu(screen.getByTestId("markdown-image-preview"), { clientX: 1000, clientY: 700 });
            const menu = screen.getByTestId("chat-media-context-menu");
            expect(menu.style.left).toBe("816px");
            expect(menu.style.top).toBe("680px");
            fireEvent.click(screen.getByTestId("chat-media-copy"));
            expect((screen.getByTestId("chat-media-copy") as HTMLButtonElement).disabled).toBe(true);
            expect(menu.style.left).toBe("816px");
            expect(menu.style.top).toBe("680px");
        } finally {
            rect.mockRestore();
            if (width) Object.defineProperty(window, "innerWidth", width);
            if (height) Object.defineProperty(window, "innerHeight", height);
            vi.mocked(CopyChatMediaToClipboard).mockImplementation(async () => undefined);
        }
    });

    it("stays open while the transcript scrolls and closes on a wheel gesture", () => {
        render(<MarkdownImagePreview src={PNG} alt="roofline" />);
        fireEvent.contextMenu(screen.getByTestId("markdown-image-preview"));
        fireEvent.scroll(window);
        const menu = screen.getByTestId("chat-media-context-menu");
        fireEvent.wheel(menu, { deltaY: 40 });
        expect(screen.getByTestId("chat-media-context-menu")).toBeTruthy();
        fireEvent.wheel(window, { deltaY: 40 });
        expect(screen.queryByTestId("chat-media-context-menu")).toBeNull();
    });

    it("keeps a thrown copy error in the menu", async () => {
        document.documentElement.lang = "zh-Hans";
        vi.mocked(CopyChatMediaToClipboard).mockImplementation(() => {
            throw new Error("binding missing");
        });
        try {
            render(<MarkdownImagePreview src={PNG} alt="roofline" />);
            fireEvent.contextMenu(screen.getByTestId("markdown-image-preview"));
            fireEvent.click(screen.getByTestId("chat-media-copy"));
            await waitFor(() => expect(screen.getByTestId("chat-media-menu-error").textContent).toContain("binding missing"));
            expect((screen.getByTestId("chat-media-copy") as HTMLButtonElement).disabled).toBe(false);
            expect(screen.getByTestId("chat-media-context-menu")).toBeTruthy();
        } finally {
            vi.mocked(CopyChatMediaToClipboard).mockImplementation(async () => undefined);
        }
    });
});
