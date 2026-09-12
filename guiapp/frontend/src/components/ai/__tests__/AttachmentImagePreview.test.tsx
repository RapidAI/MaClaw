import { fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AttachmentImageThumbnail } from "../AttachmentImagePreview";
import { lightTheme } from "../aiAssistantPanelTheme";
import { AIAssistantAttachmentFullDataURL, ImportMobileDocumentFromPath, ShowItemInFolder } from "../../../../wailsjs/go/main/App";

vi.mock("../../../../wailsjs/go/main/App", () => ({
    AIAssistantAttachmentFullDataURL: vi.fn(async () => "data:image/png;base64,FULL"),
    ImportMobileDocumentFromPath: vi.fn(async () => ({})),
    ShowItemInFolder: vi.fn(async () => undefined),
}));

const fullDataURL = vi.mocked(AIAssistantAttachmentFullDataURL);
const importToLibrary = vi.mocked(ImportMobileDocumentFromPath);
const showInFolder = vi.mocked(ShowItemInFolder);
const THUMBNAIL = "data:image/png;base64,THUMB";
const FILE_PATH = "D:\\shots\\screen.png";

function renderThumbnail(overrides: { src?: string; lang?: string; filePath?: string } = {}) {
    return render(
        <AttachmentImageThumbnail
            src={overrides.src ?? THUMBNAIL}
            filePath={overrides.filePath ?? FILE_PATH}
            fileName="screen.png"
            lang={overrides.lang ?? "zh"}
            theme={lightTheme}
            frameStyle={{ width: 30, height: 30 }}
        />,
    );
}

describe("AttachmentImageThumbnail", () => {
    afterEach(() => {
        fullDataURL.mockClear();
        fullDataURL.mockResolvedValue("data:image/png;base64,FULL");
        importToLibrary.mockClear();
        showInFolder.mockClear();
    });

    it("opens a full-image preview when the thumbnail is clicked", async () => {
        const { getByTestId, queryByTestId } = renderThumbnail();

        expect(queryByTestId("attachment-image-preview-overlay")).toBeNull();
        fireEvent.click(getByTestId("attachment-image-thumbnail"));

        expect(getByTestId("attachment-image-preview-dialog").getAttribute("aria-modal")).toBe("true");
        expect(getByTestId("attachment-image-preview-header").textContent).toContain("screen.png");
        expect(getByTestId("attachment-image-preview-image").style.maxHeight).toBe("100%");
        // The panel slides in from the right edge rather than appearing centered.
        expect(getByTestId("attachment-image-preview-dialog").style.transition).toContain("transform");
        expect(getByTestId("attachment-image-preview-image").getAttribute("src")).toBe(THUMBNAIL);
        await waitFor(() => {
            expect(getByTestId("attachment-image-preview-image").getAttribute("src")).toBe("data:image/png;base64,FULL");
        });
        expect(fullDataURL).toHaveBeenCalledWith(FILE_PATH);
        await waitFor(() => {
            expect(getByTestId("attachment-image-preview-dialog").style.transform).toBe("translateX(0)");
        });
    });

    it("keeps the file path as hover text while naming the click action for assistive tech", () => {
        const { getByTestId } = render(
            <AttachmentImageThumbnail
                src={THUMBNAIL}
                filePath={FILE_PATH}
                fileName="screen.png"
                lang="zh"
                theme={lightTheme}
                title={FILE_PATH}
                frameStyle={{ width: 30, height: 30 }}
            />,
        );

        const thumbnail = getByTestId("attachment-image-thumbnail");
        expect(thumbnail.getAttribute("title")).toBe(FILE_PATH);
        expect(thumbnail.getAttribute("aria-label")).toBe("预览图片 screen.png");
    });

    it("closes the preview from the close button", async () => {
        const { getByTestId, queryByTestId } = renderThumbnail();
        fireEvent.click(getByTestId("attachment-image-thumbnail"));

        const close = getByTestId("attachment-image-preview-close");
        expect(close.getAttribute("aria-label")).toBe("关闭");
        fireEvent.click(close);

        await waitFor(() => expect(queryByTestId("attachment-image-preview-overlay")).toBeNull());
        expect(document.body.style.overflow).toBe("");
    });

    it("closes the preview on Escape and on a backdrop click", async () => {
        const { getByTestId, queryByTestId } = renderThumbnail({ lang: "en" });

        fireEvent.click(getByTestId("attachment-image-thumbnail"));
        fireEvent.keyDown(document, { key: "Escape" });
        await waitFor(() => expect(queryByTestId("attachment-image-preview-overlay")).toBeNull());
        expect(getByTestId("attachment-image-thumbnail")).toBe(document.activeElement);

        fireEvent.click(getByTestId("attachment-image-thumbnail"));
        const overlay = getByTestId("attachment-image-preview-overlay");
        fireEvent.mouseDown(overlay);
        fireEvent.click(overlay);
        await waitFor(() => expect(queryByTestId("attachment-image-preview-overlay")).toBeNull());
    });

    it("keeps the preview open when the image itself is clicked", () => {
        const { getByTestId } = renderThumbnail();
        fireEvent.click(getByTestId("attachment-image-thumbnail"));

        const dialog = getByTestId("attachment-image-preview-dialog");
        fireEvent.mouseDown(dialog);
        fireEvent.click(dialog);

        expect(getByTestId("attachment-image-preview-overlay")).toBeTruthy();
    });

    it("keeps the thumbnail visible and explains the failure when the full image cannot load", async () => {
        fullDataURL.mockRejectedValueOnce(new Error("unreadable"));
        const { getByTestId } = renderThumbnail();

        fireEvent.click(getByTestId("attachment-image-thumbnail"));

        await waitFor(() => {
            expect(getByTestId("attachment-image-preview-status").textContent).toBe("无法加载原图");
        });
        expect(getByTestId("attachment-image-preview-image").getAttribute("src")).toBe(THUMBNAIL);
    });

    it("still loads from the host for a pasted object URL, which the composer revokes after send", async () => {
        const { getByTestId } = renderThumbnail({ src: "blob:maclaw/pasted-image" });

        fireEvent.click(getByTestId("attachment-image-thumbnail"));

        await waitFor(() => {
            expect(getByTestId("attachment-image-preview-image").getAttribute("src")).toBe("data:image/png;base64,FULL");
        });
        expect(fullDataURL).toHaveBeenCalledWith(FILE_PATH);
    });

    it("keeps Escape and Tab from reaching handlers behind the overlay", async () => {
        const behindTheOverlay = vi.fn();
        document.addEventListener("keydown", behindTheOverlay);
        const { getByTestId, queryByTestId } = renderThumbnail();

        try {
            fireEvent.click(getByTestId("attachment-image-thumbnail"));
            fireEvent.keyDown(document, { key: "Tab" });
            expect(behindTheOverlay).not.toHaveBeenCalled();
            // Close starts focused; Tab wraps to the first overlay control.
            expect(getByTestId("preview-file-action-upload")).toBe(document.activeElement);

            fireEvent.keyDown(document, { key: "Escape" });
            await waitFor(() => expect(queryByTestId("attachment-image-preview-overlay")).toBeNull());
            expect(behindTheOverlay).not.toHaveBeenCalled();
        } finally {
            document.removeEventListener("keydown", behindTheOverlay);
        }
    });

    it("omits the file actions when there is no saved file to act on", () => {
        const { getByTestId, queryByTestId } = renderThumbnail({ filePath: "" });

        fireEvent.click(getByTestId("attachment-image-thumbnail"));

        expect(queryByTestId("preview-file-actions")).toBeNull();
        expect(fullDataURL).not.toHaveBeenCalled();
    });

    it("reveals the saved file in its folder instead of opening the OS image viewer", () => {
        const { getByTestId } = renderThumbnail();
        fireEvent.click(getByTestId("attachment-image-thumbnail"));

        const close = getByTestId("attachment-image-preview-close");
        const upload = getByTestId("preview-file-action-upload");
        const share = getByTestId("preview-file-action-share");
        const reveal = getByTestId("preview-file-action-reveal");
        expect(close).toBe(document.activeElement);
        fireEvent.keyDown(document, { key: "Tab" });
        expect(upload).toBe(document.activeElement);
        fireEvent.keyDown(document, { key: "Tab" });
        expect(share).toBe(document.activeElement);
        fireEvent.keyDown(document, { key: "Tab" });
        expect(reveal).toBe(document.activeElement);
        fireEvent.keyDown(document, { key: "Tab" });
        expect(close).toBe(document.activeElement);
        fireEvent.keyDown(document, { key: "Tab", shiftKey: true });
        expect(reveal).toBe(document.activeElement);

        fireEvent.click(reveal);
        expect(showInFolder).toHaveBeenCalledWith(FILE_PATH);
    });

    it("uploads the saved file to the mobile documents library", async () => {
        const { getByTestId } = renderThumbnail();
        fireEvent.click(getByTestId("attachment-image-thumbnail"));

        fireEvent.click(getByTestId("preview-file-action-upload"));

        await waitFor(() => expect(importToLibrary).toHaveBeenCalledWith(FILE_PATH));
        await waitFor(() => expect(getByTestId("preview-file-actions-notice").textContent).toBe("已上传到文稿库"));
    });

    it("shares the saved file by copying its path to the clipboard", async () => {
        const writeText = vi.fn(async () => undefined);
        Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
        const { getByTestId } = renderThumbnail();
        fireEvent.click(getByTestId("attachment-image-thumbnail"));

        fireEvent.click(getByTestId("preview-file-action-share"));

        await waitFor(() => expect(writeText).toHaveBeenCalledWith(FILE_PATH));
        await waitFor(() => expect(getByTestId("preview-file-actions-notice").textContent).toBe("已复制文件路径，可粘贴分享"));
    });

    it("drops an undisplayable placeholder instead of showing a broken image", () => {
        const { getByTestId, queryByTestId } = renderThumbnail({ src: "blob:maclaw/revoked" });

        fireEvent.click(getByTestId("attachment-image-thumbnail"));
        fireEvent.error(getByTestId("attachment-image-preview-image"));

        expect(queryByTestId("attachment-image-preview-image")).toBeNull();
        expect(getByTestId("attachment-image-preview-fallback")).toBeTruthy();
        expect(getByTestId("attachment-image-preview-status").textContent).toBe("正在加载原图…");
    });

    it("skips the slide animation when the OS requests reduced motion", async () => {
        const original = (window as { matchMedia?: unknown }).matchMedia;
        (window as { matchMedia?: unknown }).matchMedia = (query: string) => ({
            matches: true,
            media: query,
            onchange: null,
            addEventListener: vi.fn(),
            removeEventListener: vi.fn(),
            addListener: vi.fn(),
            removeListener: vi.fn(),
            dispatchEvent: vi.fn(),
        });
        try {
            const { getByTestId, queryByTestId } = renderThumbnail();
            fireEvent.click(getByTestId("attachment-image-thumbnail"));

            expect(getByTestId("attachment-image-preview-dialog").style.transition).toBe("transform 0ms ease");
            fireEvent.keyDown(document, { key: "Escape" });
            await waitFor(() => expect(queryByTestId("attachment-image-preview-overlay")).toBeNull());
        } finally {
            (window as { matchMedia?: unknown }).matchMedia = original;
        }
    });
});
