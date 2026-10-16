import { fireEvent, render, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { PptxPreviewPanel, isPptxFileName } from "../PptxPreviewPanel";
import type { CodePreviewTheme } from "../FileTabBar";
import { AIAssistantAttachmentFullDataURL, PptxPreviewEnsure, PptxSlideThumbnailDataURL } from "../../../../wailsjs/go/main/App";

vi.mock("../../../../wailsjs/go/main/App", () => ({
    AIAssistantAttachmentFullDataURL: vi.fn(async (path: string) => `data:image/png;base64,full:${path}`),
    PptxSlideThumbnailDataURL: vi.fn(async (path: string) => `data:image/png;base64,thumb:${path}`),
    PptxPreviewEnsure: vi.fn(async () => ({
        file_path: "D:\\decks\\demo.pptx",
        output_dir: "D:\\decks\\demo_preview",
        slide_count: 3,
        rendered_count: 3,
        format: "png",
        images: ["D:\\decks\\demo_preview\\slide_001.png", "D:\\decks\\demo_preview\\slide_002.png", "D:\\decks\\demo_preview\\slide_003.png"],
    })),
}));

const ensure = vi.mocked(PptxPreviewEnsure);
const fullDataURL = vi.mocked(AIAssistantAttachmentFullDataURL);
const thumbDataURL = vi.mocked(PptxSlideThumbnailDataURL);

const theme = {
    bg: "#ffffff",
    tabBg: "#f8fafc",
    border: "#e5e7eb",
    textMuted: "#6b7280",
    tabActiveText: "#2563eb",
    diffDeleteText: "#dc2626",
    lineNumBg: "#f1f5f9",
} as CodePreviewTheme;

function renderPanel(lang = "zh") {
    return render(<PptxPreviewPanel absPath="D:\decks\demo.pptx" theme={theme} lang={lang} />);
}

describe("isPptxFileName", () => {
    it("matches .pptx case-insensitively and rejects other names", () => {
        expect(isPptxFileName("demo.pptx")).toBe(true);
        expect(isPptxFileName("demo.PPTX")).toBe(true);
        expect(isPptxFileName("demo.ppt")).toBe(false);
        expect(isPptxFileName("demo.pptx.txt")).toBe(false);
    });
});

describe("PptxPreviewPanel", () => {
    it("renders thumbnails on the left and selects the first page by default", async () => {
        const { getByTestId, getAllByTestId } = renderPanel();

        expect(getByTestId("pptx-preview-loading")).toBeTruthy();
        await waitFor(() => expect(getByTestId("pptx-preview-panel")).toBeTruthy());

        const thumbnails = getAllByTestId("pptx-preview-thumbnail");
        expect(thumbnails).toHaveLength(3);
        expect(thumbnails[0].getAttribute("data-active")).toBe("true");
        expect(thumbnails[1].getAttribute("data-active")).toBe("false");

        // The strip uses the bounded thumbnail binding for every page…
        await waitFor(() => expect(thumbDataURL).toHaveBeenCalledTimes(3));
        // …while the full-fidelity slide loads only for the selected page.
        await waitFor(() => {
            expect(getByTestId("pptx-preview-slide").getAttribute("src")).toBe("data:image/png;base64,full:D:\\decks\\demo_preview\\slide_001.png");
        });
        expect(fullDataURL).toHaveBeenCalledTimes(1);
        expect(getByTestId("pptx-preview-page-indicator").textContent).toBe("1 / 3");
        expect(ensure).toHaveBeenCalledWith("D:\\decks\\demo.pptx");
    });

    it("shows the clicked page large", async () => {
        const { getByTestId, getAllByTestId } = renderPanel();
        await waitFor(() => expect(getByTestId("pptx-preview-slide")).toBeTruthy());

        fireEvent.click(getAllByTestId("pptx-preview-thumbnail")[1]);

        await waitFor(() => {
            expect(getByTestId("pptx-preview-slide").getAttribute("src")).toBe("data:image/png;base64,full:D:\\decks\\demo_preview\\slide_002.png");
        });
        expect(getByTestId("pptx-preview-page-indicator").textContent).toBe("2 / 3");
        expect(getAllByTestId("pptx-preview-thumbnail")[1].getAttribute("data-active")).toBe("true");
    });

    it("holds the page with the enlarged thumbnail while full bytes load", async () => {
        fullDataURL.mockImplementation(async (path: string) => path.includes("slide_001") ? `data:image/png;base64,full:${path}` : "");
        const { getByTestId, getAllByTestId } = renderPanel();
        await waitFor(() => expect(getByTestId("pptx-preview-slide")).toBeTruthy());

        fireEvent.click(getAllByTestId("pptx-preview-thumbnail")[1]);

        await waitFor(() => expect(getByTestId("pptx-preview-page-indicator").textContent).toBe("2 / 3"));
        expect(getByTestId("pptx-preview-slide-placeholder").getAttribute("src")).toBe("data:image/png;base64,thumb:D:\\decks\\demo_preview\\slide_002.png");
        expect(getByTestId("pptx-preview-page-indicator").textContent).toBe("2 / 3");
    });

    it("moves the selection with arrow keys", async () => {
        const { getByTestId } = renderPanel();
        await waitFor(() => expect(getByTestId("pptx-preview-slide")).toBeTruthy());

        const strip = getByTestId("pptx-preview-thumbnails");
        fireEvent.keyDown(strip, { key: "ArrowDown" });
        await waitFor(() => expect(getByTestId("pptx-preview-page-indicator").textContent).toBe("2 / 3"));
        fireEvent.keyDown(strip, { key: "End" });
        await waitFor(() => expect(getByTestId("pptx-preview-page-indicator").textContent).toBe("3 / 3"));
        fireEvent.keyDown(strip, { key: "ArrowDown" });
        expect(getByTestId("pptx-preview-page-indicator").textContent).toBe("3 / 3");
        fireEvent.keyDown(strip, { key: "Home" });
        await waitFor(() => expect(getByTestId("pptx-preview-page-indicator").textContent).toBe("1 / 3"));
    });

    it("surfaces a render failure", async () => {
        ensure.mockRejectedValueOnce(new Error("读取 PPTX 失败"));
        const { getByTestId } = renderPanel();

        await waitFor(() => expect(getByTestId("pptx-preview-error").textContent).toContain("读取 PPTX 失败"));
    });

    it("reports decks without renderable slides", async () => {
        ensure.mockResolvedValueOnce({ images: [] });
        const { getByTestId } = renderPanel();

        await waitFor(() => expect(getByTestId("pptx-preview-error").textContent).toBe("该 PPT 没有可预览的页面"));
    });
});
